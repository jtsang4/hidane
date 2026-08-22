import { createDecipheriv, createHash } from "node:crypto";
import type { Hono } from "hono";
import { appendEvent } from "../kernel/events.js";
import { createBinding, findByChannelRef, findByWorkItem } from "../kernel/bindings.js";
import { getWorkItem } from "../kernel/workItems.js";
import { config } from "../config.js";
import { handleUserMessage } from "../agents/primary.js";
import { handleThreadMessage } from "../agents/manager.js";

/**
 * Feishu channel binding.
 * Mapping: p2p chat top-level ↔ main thread; the reply-thread under a
 * bot-posted root message ("📋 wi_x — title") ↔ that work item's thread.
 * The connector layer only captures/normalizes; user messages then take the
 * same fast lane as web chat. Bot echoes are filtered by sender type.
 */

const FEISHU_BASE = "https://open.feishu.cn/open-apis";

/** Decrypt Feishu "encrypt key" payloads: AES-256-CBC, key=sha256(secret), iv=head 16B. */
export function decryptFeishu(encrypted: string, encryptKey: string): string {
  const buf = Buffer.from(encrypted, "base64");
  const key = createHash("sha256").update(encryptKey).digest();
  const decipher = createDecipheriv("aes-256-cbc", key, buf.subarray(0, 16));
  return Buffer.concat([decipher.update(buf.subarray(16)), decipher.final()]).toString("utf8");
}

interface FeishuMessageEvent {
  sender?: {
    sender_type?: string;
    sender_id?: { open_id?: string };
  };
  message?: {
    message_id?: string;
    chat_id?: string;
    chat_type?: string;
    message_type?: string;
    root_id?: string;
    content?: string;
  };
}

export function extractText(event: FeishuMessageEvent): string {
  if (event.message?.message_type !== "text") return "";
  try {
    const parsed = JSON.parse(event.message.content ?? "{}") as { text?: string };
    return (parsed.text ?? "").replace(/@_user_\d+\s*/g, "").trim();
  } catch {
    return "";
  }
}

/** Bot echoes and non-user senders never re-enter the loop. */
export function isUserMessage(event: FeishuMessageEvent): boolean {
  return event.sender?.sender_type === "user";
}

interface TokenCache {
  token: string;
  expiresAt: number;
}

let tokenCache: TokenCache | undefined;

export class FeishuApi {
  constructor(
    private appId: string,
    private appSecret: string,
  ) {}

  async token(): Promise<string> {
    if (tokenCache && tokenCache.expiresAt > Date.now() + 60_000) {
      return tokenCache.token;
    }
    const res = await fetch(`${FEISHU_BASE}/auth/v3/tenant_access_token/internal`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ app_id: this.appId, app_secret: this.appSecret }),
    });
    const body = (await res.json()) as {
      code: number;
      tenant_access_token?: string;
      expire?: number;
      msg?: string;
    };
    if (body.code !== 0 || !body.tenant_access_token) {
      throw new Error(`feishu token failed: ${body.code} ${body.msg}`);
    }
    tokenCache = {
      token: body.tenant_access_token,
      expiresAt: Date.now() + (body.expire ?? 3600) * 1000,
    };
    return tokenCache.token;
  }

  private async call<T>(path: string, payload: unknown): Promise<T> {
    const res = await fetch(`${FEISHU_BASE}${path}`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${await this.token()}`,
      },
      body: JSON.stringify(payload),
    });
    const body = (await res.json()) as { code: number; msg?: string; data?: T };
    if (body.code !== 0) throw new Error(`feishu api ${path} failed: ${body.code} ${body.msg}`);
    return body.data as T;
  }

  /** Send a top-level text message to a chat; returns message_id. */
  async sendText(chatId: string, text: string): Promise<string> {
    const data = await this.call<{ message_id: string }>(
      "/im/v1/messages?receive_id_type=chat_id",
      {
        receive_id: chatId,
        msg_type: "text",
        content: JSON.stringify({ text }),
      },
    );
    return data.message_id;
  }

  /** Reply inside the thread rooted at a message. */
  async replyInThread(rootMessageId: string, text: string): Promise<string> {
    const data = await this.call<{ message_id: string }>(
      `/im/v1/messages/${rootMessageId}/reply`,
      {
        msg_type: "text",
        content: JSON.stringify({ text }),
        reply_in_thread: true,
      },
    );
    return data.message_id;
  }
}

export function feishuEnabled(): boolean {
  return Boolean(config.feishuAppId && config.feishuAppSecret);
}

function api(): FeishuApi {
  return new FeishuApi(config.feishuAppId!, config.feishuAppSecret!);
}

const seenEvents = new Set<string>();

function dedup(eventId: string): boolean {
  if (seenEvents.has(eventId)) return true;
  seenEvents.add(eventId);
  if (seenEvents.size > 1000) {
    for (const id of [...seenEvents].slice(0, 500)) seenEvents.delete(id);
  }
  return false;
}

/** Post-outcome delivery: mirror runtime replies back onto the Feishu surface. */
async function deliverOutcome(
  chatId: string,
  outcome: { action: string; reply: string; workItemId?: string | undefined },
): Promise<void> {
  const feishu = api();
  if (outcome.workItemId) {
    let binding = await findByWorkItem("feishu", outcome.workItemId);
    if (!binding) {
      const item = await getWorkItem(outcome.workItemId);
      const rootId = await feishu.sendText(chatId, `📋 ${item.id} — ${item.title}`);
      binding = await createBinding({
        channel: "feishu",
        kind: "work_item",
        workItemId: item.id,
        chatId,
        rootId,
      });
      await appendEvent({
        source: "connector:feishu",
        kind: "binding.created",
        threadId: item.threadId,
        workItemId: item.id,
        payload: { chatId, rootId },
      });
    }
    if (binding.rootId) {
      await feishu.replyInThread(binding.rootId, outcome.reply.slice(0, 4000));
      return;
    }
  }
  await feishu.sendText(chatId, outcome.reply.slice(0, 4000));
}

async function handleMessageEvent(event: FeishuMessageEvent): Promise<void> {
  const chatId = event.message?.chat_id;
  const text = extractText(event);
  if (!chatId || !text) return;

  await appendEvent({
    source: "connector:feishu",
    kind: "connector.feishu",
    payload: {
      chatId,
      rootId: event.message?.root_id ?? null,
      messageId: event.message?.message_id ?? null,
      text: text.slice(0, 2000),
    },
  });

  const rootId = event.message?.root_id ?? null;
  const binding = rootId ? await findByChannelRef("feishu", chatId, rootId) : undefined;

  if (binding?.kind === "work_item" && binding.workItemId) {
    // Reply inside a bound work-item thread → straight to its Manager.
    const item = await getWorkItem(binding.workItemId);
    await appendEvent({
      source: "connector:feishu",
      kind: "user.message",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text },
    });
    const reply = await handleThreadMessage(item.id, text);
    if (binding.rootId) await api().replyInThread(binding.rootId, reply.slice(0, 4000));
    return;
  }

  // Top-level message → main-thread fast lane.
  const outcome = await handleUserMessage(text, "connector:feishu");
  await deliverOutcome(chatId, outcome);
}

/** Mount the Feishu event endpoint (challenge + message events). */
export function registerFeishu(app: Hono): void {
  app.post("/feishu/events", async (c) => {
    let payload = (await c.req.json().catch(() => ({}))) as Record<string, unknown>;

    if (typeof payload["encrypt"] === "string") {
      if (!config.feishuEncryptKey) {
        return c.json({ ok: false, error: "encrypt key not configured" }, 400);
      }
      try {
        payload = JSON.parse(
          decryptFeishu(payload["encrypt"], config.feishuEncryptKey),
        ) as Record<string, unknown>;
      } catch {
        return c.json({ ok: false, error: "decrypt failed" }, 400);
      }
    }

    // URL verification handshake.
    if (payload["type"] === "url_verification") {
      if (
        config.feishuVerificationToken &&
        payload["token"] !== config.feishuVerificationToken
      ) {
        return c.json({ ok: false }, 401);
      }
      return c.json({ challenge: payload["challenge"] });
    }

    const header = payload["header"] as
      | { event_id?: string; event_type?: string; token?: string }
      | undefined;
    if (
      config.feishuVerificationToken &&
      header?.token !== config.feishuVerificationToken
    ) {
      return c.json({ ok: false }, 401);
    }
    if (header?.event_id && dedup(header.event_id)) return c.json({ ok: true });

    if (header?.event_type === "im.message.receive_v1" && feishuEnabled()) {
      const event = payload["event"] as FeishuMessageEvent;
      if (isUserMessage(event)) {
        // Respond fast; processing continues asynchronously.
        void handleMessageEvent(event).catch(async (err) => {
          await appendEvent({
            source: "connector:feishu",
            kind: "agent.error",
            payload: { error: String(err) },
          }).catch(() => {});
        });
      }
    }
    return c.json({ ok: true });
  });
}
