import * as lark from "@larksuiteoapi/node-sdk";
import type { Hono } from "hono";
import { appendEvent } from "../kernel/events.js";
import {
  createBinding,
  findByChannelRef,
  findByWorkItem,
  findMainBinding,
} from "../kernel/bindings.js";
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

export interface InboundImage {
  data: string;
  mimeType: string;
}

/** Image keys carried by a Feishu message (image, or post with embedded images). */
export function imageKeys(messageType: string, content: string): string[] {
  try {
    const parsed = JSON.parse(content) as Record<string, unknown>;
    if (messageType === "image") {
      const key = parsed["image_key"];
      return typeof key === "string" ? [key] : [];
    }
    if (messageType === "post") {
      // Rich post: content is a matrix of element rows.
      const rows = (parsed["content"] ?? []) as unknown[][];
      return rows
        .flat()
        .filter(
          (el): el is { tag: string; image_key: string } =>
            typeof el === "object" &&
            el !== null &&
            (el as { tag?: string }).tag === "img" &&
            typeof (el as { image_key?: string }).image_key === "string",
        )
        .map((el) => el.image_key);
    }
    return [];
  } catch {
    return [];
  }
}

/** Content type from magic bytes — the download response headers are not
 *  reliable, and a jpeg labelled image/png is rejected by the vision model. */
export function sniffMime(buf: Buffer, fallback: string): string {
  if (buf.length >= 3 && buf[0] === 0xff && buf[1] === 0xd8 && buf[2] === 0xff) {
    return "image/jpeg";
  }
  if (buf.length >= 8 && buf.subarray(0, 8).toString("hex") === "89504e470d0a1a0a") {
    return "image/png";
  }
  if (buf.length >= 6 && buf.subarray(0, 6).toString("ascii").startsWith("GIF8")) {
    return "image/gif";
  }
  if (
    buf.length >= 12 &&
    buf.subarray(0, 4).toString("ascii") === "RIFF" &&
    buf.subarray(8, 12).toString("ascii") === "WEBP"
  ) {
    return "image/webp";
  }
  return fallback;
}

/**
 * Feishu's real reason for a failed download. The SDK surfaces only
 * "Request failed with status code 400" while the body carries the actual
 * cause — a guessed cause is worse than none: "check the im:resource scope"
 * sent debugging down the wrong path when the message had simply been recalled.
 */
async function describeLarkError(err: unknown): Promise<string> {
  const base = err instanceof Error ? err.message : String(err);
  const body = (err as { response?: { data?: unknown } })?.response?.data;
  try {
    let raw: string | undefined;
    if (typeof body === "string") raw = body;
    else if (body && typeof (body as { on?: unknown }).on === "function") {
      const chunks: Buffer[] = [];
      for await (const c of body as AsyncIterable<Buffer>) chunks.push(Buffer.from(c));
      raw = Buffer.concat(chunks).toString("utf8");
    } else if (body && typeof body === "object") raw = JSON.stringify(body);
    if (!raw) return base;
    const parsed = JSON.parse(raw) as { code?: number; msg?: string };
    return parsed.msg ? `${base} (feishu ${parsed.code}: ${parsed.msg})` : base;
  } catch {
    return base;
  }
}

export interface FetchImagesResult {
  images: InboundImage[];
  /** Per-key failure reasons — surfaced, never swallowed: a missing `im:resource`
   *  scope is otherwise indistinguishable from "the user sent no image". */
  failures: string[];
}

/**
 * Stand-in text for a message that carries no text of its own, so the agent is
 * told what actually arrived instead of receiving nothing. Truthful by
 * construction: a failed download says so rather than pretending an image is
 * attached (the model then denies seeing one, and the user cannot tell why).
 */
export const IMAGE_ONLY_TEXT = "(图片消息，请查看附带图片)";

export function describeMessage(
  messageType: string,
  keys: string[],
  images: InboundImage[],
  failures: string[],
): string {
  if (images.length > 0) {
    return failures.length > 0
      ? `(图片消息：附带 ${images.length} 张图片，另有 ${failures.length} 张下载失败)`
      : IMAGE_ONLY_TEXT;
  }
  if (keys.length > 0) {
    return `(收到 ${keys.length} 张图片，但下载失败，无法查看内容)`;
  }
  return `(收到一条 ${messageType || "unknown"} 类型的消息，暂不支持解析内容)`;
}

/** Download message images so the vision model actually receives them. */
async function fetchImages(
  messageId: string,
  keys: string[],
): Promise<FetchImagesResult> {
  const images: InboundImage[] = [];
  const failures: string[] = [];
  for (const key of keys.slice(0, 4)) {
    try {
      const res = await larkClient().im.messageResource.get({
        params: { type: "image" },
        path: { message_id: messageId, file_key: key },
      });
      const chunks: Buffer[] = [];
      for await (const chunk of res.getReadableStream()) {
        chunks.push(Buffer.from(chunk as Buffer));
      }
      const buf = Buffer.concat(chunks);
      if (buf.length === 0) throw new Error("empty body");
      const header =
        (res.headers?.["content-type"] as string | undefined)?.split(";")[0] ?? "";
      images.push({
        data: buf.toString("base64"),
        mimeType: sniffMime(buf, header.startsWith("image/") ? header : "image/png"),
      });
    } catch (err) {
      // A failed image must not sink the whole message — but it must be visible.
      failures.push(`${key}: ${await describeLarkError(err)}`);
    }
  }
  return { images, failures };
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

/**
 * Outbound push without an inbound trigger (scheduled prompts): deliver to the
 * main-thread binding if one exists. Quiet no-op otherwise — a schedule must
 * work on a deployment that has no Feishu at all.
 */
export async function deliverToMain(text: string): Promise<boolean> {
  if (!feishuEnabled()) return false;
  const binding = await findMainBinding("feishu");
  if (!binding) return false;
  await sendText(binding.chatId, text.slice(0, 4000));
  return true;
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
  if (!chatId) return;
  const messageType = data.message?.message_type ?? "";
  const rawContent = data.message?.content ?? "";
  const keys = imageKeys(messageType, rawContent);
  const { images, failures } =
    keys.length > 0
      ? await fetchImages(data.message.message_id, keys)
      : { images: [] as InboundImage[], failures: [] as string[] };

  // Connectors capture, never judge. A message we cannot fully read still gets
  // recorded and still reaches the agent with an honest description — dropping
  // it made the user's image vanish with no trace anywhere in the log.
  const text = extractText(rawContent) || describeMessage(messageType, keys, images, failures);

  await appendEvent({
    source: "connector:feishu",
    kind: "connector.feishu",
    payload: {
      chatId,
      rootId: data.message.root_id ?? null,
      messageId: data.message.message_id ?? null,
      messageType,
      imageCount: images.length,
      ...(failures.length > 0 ? { imageFailures: failures } : {}),
      text: text.slice(0, 2000),
    },
  });

  if (failures.length > 0) {
    await appendEvent({
      source: "connector:feishu",
      kind: "agent.error",
      payload: {
        error: `failed to download ${failures.length} image(s) from Feishu`,
        detail: failures.slice(0, 4),
        messageId: data.message.message_id ?? null,
      },
    });
  }

  // The log takes everything; the model wakes only for input it can act on.
  // Stickers, audio and Feishu's own system notices are recorded and stop here.
  if (!extractText(rawContent) && keys.length === 0) return;

  // Remember the p2p chat as the main-thread binding. Inbound routing never
  // needed the row (top-level p2p is implicitly main), but outbound-without-
  // inbound does: a scheduled reminder cannot reach a chat nobody recorded.
  if (data.message.chat_type === "p2p" && !(await findByChannelRef("feishu", chatId, null))) {
    await createBinding({ channel: "feishu", kind: "main", chatId });
  }

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

  const outcome = await handleUserMessage(text, "connector:feishu", images);
  await deliverOutcome(chatId, outcome);
}

/**
 * Feishu retries an event until it gets a 200, and the SDK dispatcher does not
 * deduplicate. Without this guard a retry would run the whole LLM/worker chain
 * a second time — an at-most-once gate on event_id is required.
 */
const seenEventIds = new Set<string>();

export function isDuplicateEvent(eventId: string | undefined): boolean {
  if (!eventId) return false;
  if (seenEventIds.has(eventId)) return true;
  seenEventIds.add(eventId);
  if (seenEventIds.size > 2000) {
    for (const id of [...seenEventIds].slice(0, 1000)) seenEventIds.delete(id);
  }
  return false;
}

let dispatcher: lark.EventDispatcher | undefined;
function eventDispatcher(): lark.EventDispatcher {
  if (dispatcher) return dispatcher;
  dispatcher = new lark.EventDispatcher({
    verificationToken: config.feishuVerificationToken ?? "",
    encryptKey: config.feishuEncryptKey ?? "",
  }).register({
    "im.message.receive_v1": async (data) => {
      const eventId = (data as { event_id?: string }).event_id;
      if (isDuplicateEvent(eventId)) return { code: 0 };
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

export type EventAuthVerdict = "ok" | "reject" | "unconfigured";

/**
 * Authenticate an inbound event ourselves.
 *
 * The SDK's own check is a no-op for plaintext schema-2.0 events — its
 * `checkIsEventValidated` returns true whenever no encrypt key is configured —
 * which left `/feishu/events` open to anyone who knew the URL, on an endpoint
 * that spends model tokens and runs worker executions. The guarantee must not
 * depend on SDK internals, so it is enforced here.
 */
export function verifyEventAuth(body: Record<string, unknown>): EventAuthVerdict {
  // An encrypted envelope authenticates itself: forging one needs the key.
  if (config.feishuEncryptKey && typeof body["encrypt"] === "string") return "ok";
  const expected = config.feishuVerificationToken;
  if (!expected) return "unconfigured";
  const header = body["header"] as { token?: unknown } | undefined;
  const got =
    typeof header?.token === "string"
      ? header.token
      : typeof body["token"] === "string"
        ? (body["token"] as string)
        : "";
  return got === expected ? "ok" : "reject";
}

/** Mount the Feishu event endpoint. The SDK owns decryption, the challenge
 *  handshake and dispatch; authenticity is enforced here. */
export function registerFeishu(app: Hono): void {
  app.post("/feishu/events", async (c) => {
    if (!feishuEnabled()) return c.json({ ok: false, error: "feishu disabled" }, 503);
    const body = (await c.req.json().catch(() => ({}))) as Record<string, unknown>;
    // url_verification handshake (handles encrypted envelopes too).
    const { isChallenge, challenge } = lark.generateChallenge(body, {
      encryptKey: config.feishuEncryptKey ?? "",
    });
    if (isChallenge) return c.json(challenge);

    const verdict = verifyEventAuth(body);
    if (verdict !== "ok") {
      // Fail closed, and leave evidence: a silently-dropped real message is
      // exactly the failure mode this connector already had once.
      await appendEvent({
        source: "connector:feishu",
        kind: "agent.error",
        payload: {
          error:
            verdict === "unconfigured"
              ? "rejected a Feishu event: set FEISHU_VERIFICATION_TOKEN or FEISHU_ENCRYPT_KEY — the endpoint runs agent executions and must not be open"
              : "rejected a Feishu event: verification token mismatch",
          eventId: (body["header"] as { event_id?: string } | undefined)?.event_id ?? null,
          // Enough to tell "wrong value" from "Feishu did not send the field at
          // all" without writing the token itself into the log.
          tokenFieldPresent:
            typeof (body["header"] as { token?: unknown } | undefined)?.token === "string" ||
            typeof body["token"] === "string",
          bodyKeys: Object.keys(body).slice(0, 8),
        },
      }).catch(() => {});
      return c.json({ ok: false, error: "unauthorized" }, 401);
    }

    // Real events: dispatcher decrypts and routes to the handler.
    const result = (await eventDispatcher().invoke(body)) as Record<string, unknown>;
    return c.json(result ?? { code: 0 });
  });
}
