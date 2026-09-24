import { appendEvent, getEvent, listEvents, type HidaneEvent } from "../kernel/events.js";
import { post, rootOf } from "../kernel/mailbox.js";
import {
  getWorkItem,
  listChildren,
  setWorkItemStatus,
  type WorkItem,
} from "../kernel/workItems.js";
import { PRIMARY, managerAddress } from "./addresses.js";
import { storeImages, type InboundImage } from "./inbox.js";

export type AttributionSource = "explicit" | "focus" | "model" | "user";

export interface InboundMessage {
  text: string;
  source: string;
  images?: InboundImage[] | undefined;
  /** Work item the person addressed directly (focused card, Feishu thread). */
  target?: string | undefined;
  /** Event the person replied to; its work item becomes the target. */
  replyTo?: string | undefined;
  /** True when the target came from what was focused rather than an explicit pick. */
  focus?: boolean | undefined;
  /** Channel coordinates the outbox needs to answer on the same surface. */
  channel?: Record<string, unknown> | undefined;
}

/**
 * The one door every person's message comes through, whatever the channel.
 * Recording and delivery are the same append: the message lands on the main
 * thread and is addressed to whoever should read it. Nothing here waits for an
 * answer — answers arrive later as events.
 *
 * Attribution is decided cheapest-first: an explicit target or a reply to
 * something that belongs to a work item needs no model at all; only messages
 * with neither go to the Primary to be routed.
 */
export async function submitMessage(message: InboundMessage): Promise<HidaneEvent> {
  let targetId = message.target;
  let by: AttributionSource | undefined = targetId
    ? message.focus
      ? "focus"
      : "explicit"
    : undefined;
  let answers: string | undefined;
  if (message.replyTo) {
    const ref = await getEvent(message.replyTo);
    if (ref?.kind === "escalation") answers = ref.id;
    if (!targetId && ref?.workItemId) {
      targetId = ref.workItemId;
      by = "explicit";
    }
  }
  const images = await storeImages(message.images ?? []);
  const payload: Record<string, unknown> = {
    text: message.text,
    ...(images.length > 0 ? { images, imageCount: images.length } : {}),
    ...(message.replyTo ? { replyTo: message.replyTo } : {}),
    ...(message.channel ? { channel: message.channel } : {}),
  };

  if (targetId) {
    const item = await getWorkItem(targetId);
    const event = await appendEvent({
      source: message.source,
      kind: "user.message",
      threadId: "main",
      workItemId: item.id,
      payload: { ...payload, target: item.id },
    });
    await deliverToWorkItem(event, item, by ?? "explicit", { answers });
    return event;
  }

  const event = await post({
    source: message.source,
    kind: "user.message",
    mailbox: PRIMARY,
    lane: "interrupt",
    threadId: "main",
    payload,
  });
  if (!event) throw new Error("message could not be delivered");
  return event;
}

/**
 * Hand a main-thread message to a work item's Manager and record who decided.
 * The Manager's copy lives on the work item's thread (`forwardedFrom: main`);
 * the main-thread original stays the person's record of what they said. A
 * closed item the person talks to again is reopened.
 */
export async function deliverToWorkItem(
  message: HidaneEvent,
  item: WorkItem,
  by: AttributionSource,
  opts: {
    text?: string | undefined;
    confidence?: number | undefined;
    answers?: string | undefined;
    previous?: string | undefined;
    created?: boolean | undefined;
    source?: string | undefined;
  } = {},
): Promise<HidaneEvent | null> {
  if (item.status !== "open") {
    await setWorkItemStatus(item.id, "open", opts.source ?? "agent:primary");
  }
  await appendEvent({
    source: opts.source ?? (by === "model" ? "agent:primary" : message.source),
    kind: "message.attributed",
    threadId: "main",
    workItemId: item.id,
    causedBy: message.id,
    payload: {
      of: message.id,
      workItemId: item.id,
      title: item.title,
      by,
      ...(opts.confidence !== undefined ? { confidence: opts.confidence } : {}),
      ...(opts.previous ? { previous: opts.previous } : {}),
      ...(opts.created ? { created: true } : {}),
    },
  });
  return post({
    source: opts.source ?? (by === "model" ? "agent:primary" : message.source),
    kind: "user.message",
    mailbox: managerAddress(item.id),
    lane: "interrupt",
    threadId: item.threadId,
    workItemId: item.id,
    causedBy: message,
    payload: {
      text: opts.text ?? message.payload["text"],
      of: message.id,
      root: rootOf(message),
      forwardedFrom: "main",
      ...(Array.isArray(message.payload["images"]) ? { images: message.payload["images"] } : {}),
      ...(opts.answers ? { answers: opts.answers } : {}),
    },
  });
}

/**
 * The person moved a message to another work item (or answered "which one?").
 * The earlier decision stays in the log; the newest attribution wins.
 */
export async function reattribute(
  messageId: string,
  workItemId: string,
  source: string,
): Promise<HidaneEvent | null> {
  const message = await getEvent(messageId);
  if (!message || message.kind !== "user.message") throw new Error("message not found");
  const item = await getWorkItem(workItemId);
  const previous = (
    await listEvents({ kind: "message.attributed", payloadEquals: { key: "of", value: messageId } })
  ).at(-1)?.workItemId;
  if (previous === item.id) return null;
  return deliverToWorkItem(message, item, "user", {
    source,
    ...(previous ? { previous } : {}),
  });
}

/**
 * Status changes that close a child may settle its parent's fan-out. Every
 * path that changes a status goes through here so the parent is always told.
 */
export async function changeStatus(
  id: string,
  status: WorkItem["status"],
  source: string,
  causedBy?: HidaneEvent | undefined,
): Promise<WorkItem> {
  const item = await setWorkItemStatus(id, status, source);
  if (item.parentId && status !== "open") await notifyParentIfSettled(item, causedBy);
  return item;
}

async function notifyParentIfSettled(child: WorkItem, causedBy?: HidaneEvent): Promise<void> {
  const parentId = child.parentId;
  if (!parentId) return;
  const siblings = await listChildren(parentId);
  if (siblings.some((s) => s.status === "open")) return;
  const parent = await getWorkItem(parentId);
  const results = [];
  for (const s of siblings) {
    const last = (await listEvents({ workItemId: s.id, kind: "agent.reply", tail: 1 }))[0];
    results.push({
      workItemId: s.id,
      title: s.title,
      status: s.status,
      result: String(last?.payload["text"] ?? "").slice(0, 3000),
    });
  }
  await post({
    source: "kernel:runtime",
    kind: "children.settled",
    mailbox: managerAddress(parent.id),
    lane: "normal",
    threadId: parent.threadId,
    workItemId: parent.id,
    causedBy,
    payload: { children: results, ...(causedBy ? { root: rootOf(causedBy) } : {}) },
  });
}

/** Where a fact that bubbles goes next: the parent's Manager, or the Primary at the top. */
export function parentMailbox(item: WorkItem): string {
  return item.parentId ? managerAddress(item.parentId) : PRIMARY;
}
