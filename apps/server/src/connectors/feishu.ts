import * as lark from "@larksuiteoapi/node-sdk";
import type { Hono } from "hono";
import { appendEvent } from "../kernel/events.js";
import { createBinding, findByChannelRef, findByWorkItem } from "../kernel/bindings.js";
import { getWorkItem } from "../kernel/workItems.js";
import { config } from "../config.js";
import { handleUserMessage } from "../agents/primary.js";
import { handleThreadMessage } from "../agents/manager.js";

/**
 * Feishu channel binding, on the official SDK.
 * The SDK owns token refresh, AES decryption, challenge handshake, signature
 * verification and event dispatch — hidane keeps only its own semantics:
 * p2p chat top-level ↔ main thread; the reply-thread under a bot-posted
 * "📋 wi_x — title" root ↔ that work item's thread.
 */

export function feishuEnabled(): boolean {
  return Boolean(config.feishuAppId && config.feishuAppSecret);
}

let client: lark.Client | undefined;
function larkClient(): lark.Client {
  client ??= new lark.Client({
    appId: config.feishuAppId!,
    appSecret: config.feishuAppSecret!,
    appType: lark.AppType.SelfBuild,
    domain: lark.Domain.Feishu,
  });
  return client;
}

async function sendText(chatId: string, text: string): Promise<string> {
  const res = await larkClient().im.message.create({
    params: { receive_id_type: "chat_id" },
    data: { receive_id: chatId, msg_type: "text", content: JSON.stringify({ text }) },
  });
  return res.data?.message_id ?? "";
}

async function replyInThread(rootMessageId: string, text: string): Promise<string> {
  const res = await larkClient().im.message.reply({
    path: { message_id: rootMessageId },
    data: { msg_type: "text", content: JSON.stringify({ text }), reply_in_thread: true },
  });
  return res.data?.message_id ?? "";
}

export function extractText(content: string): string {
  try {
    const parsed = JSON.parse(content) as { text?: string };
    return (parsed.text ?? "").replace(/@_user_\d+\s*/g, "").trim();
  } catch {
    return "";
  }
}

interface MessageEventData {
  sender: { sender_type: string };
  message: {
    message_id: string;
    chat_id: string;
    chat_type: string;
    message_type: string;
    root_id?: string;
    content: string;
  };
}

/** Post-outcome delivery: mirror runtime replies back onto the Feishu surface. */
async function deliverOutcome(
  chatId: string,
  outcome: { reply: string; workItemId?: string | undefined },
): Promise<void> {
  if (outcome.workItemId) {
    let binding = await findByWorkItem("feishu", outcome.workItemId);
    if (!binding) {
      const item = await getWorkItem(outcome.workItemId);
      const rootId = await sendText(chatId, `📋 ${item.id} — ${item.title}`);
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
      await replyInThread(binding.rootId, outcome.reply.slice(0, 4000));
      return;
    }
  }
  await sendText(chatId, outcome.reply.slice(0, 4000));
}

async function handleMessageEvent(data: MessageEventData): Promise<void> {
  if (data.sender?.sender_type !== "user") return; // bot echoes never re-enter
  const chatId = data.message?.chat_id;
  const text = extractText(data.message?.content ?? "");
  if (!chatId || !text) return;

  await appendEvent({
    source: "connector:feishu",
    kind: "connector.feishu",
    payload: {
      chatId,
      rootId: data.message.root_id ?? null,
      messageId: data.message.message_id ?? null,
      text: text.slice(0, 2000),
    },
  });

  const rootId = data.message.root_id ?? null;
  const binding = rootId ? await findByChannelRef("feishu", chatId, rootId) : undefined;

  if (binding?.kind === "work_item" && binding.workItemId) {
    const item = await getWorkItem(binding.workItemId);
    await appendEvent({
      source: "connector:feishu",
      kind: "user.message",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text },
    });
    const reply = await handleThreadMessage(item.id, text);
    if (binding.rootId) await replyInThread(binding.rootId, reply.slice(0, 4000));
    return;
  }

  const outcome = await handleUserMessage(text, "connector:feishu");
  await deliverOutcome(chatId, outcome);
}

let dispatcher: lark.EventDispatcher | undefined;
function eventDispatcher(): lark.EventDispatcher {
  if (dispatcher) return dispatcher;
  dispatcher = new lark.EventDispatcher({
    verificationToken: config.feishuVerificationToken ?? "",
    encryptKey: config.feishuEncryptKey ?? "",
  }).register({
    "im.message.receive_v1": async (data) => {
      // Respond fast; the LLM path runs detached (Feishu needs a <3s ack).
      void handleMessageEvent(data as unknown as MessageEventData).catch(async (err) => {
        await appendEvent({
          source: "connector:feishu",
          kind: "agent.error",
          payload: { error: String(err) },
        }).catch(() => {});
      });
      return { code: 0 };
    },
  });
  return dispatcher;
}

/** Mount the Feishu event endpoint. The SDK owns decryption, the challenge
 *  handshake, token verification and dispatch to the registered handler. */
export function registerFeishu(app: Hono): void {
  app.post("/feishu/events", async (c) => {
    if (!feishuEnabled()) return c.json({ ok: false, error: "feishu disabled" }, 503);
    const body = (await c.req.json().catch(() => ({}))) as Record<string, unknown>;
    // url_verification handshake (handles encrypted envelopes too).
    const { isChallenge, challenge } = lark.generateChallenge(body, {
      encryptKey: config.feishuEncryptKey ?? "",
    });
    if (isChallenge) return c.json(challenge);
    // Real events: dispatcher decrypts, verifies, and routes to the handler.
    const result = (await eventDispatcher().invoke(body)) as Record<string, unknown>;
    return c.json(result ?? { code: 0 });
  });
}
