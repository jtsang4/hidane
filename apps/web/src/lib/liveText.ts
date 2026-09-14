import { SvelteMap } from "svelte/reactivity";
import { CONVERSATION_KINDS } from "./grouping.js";

/**
 * A reply being written, rendered before the event that records it exists.
 *
 * The runtime's answer is append-only and arrives as one finished `agent.reply`
 * event, which is why the chat used to sit silent and then print a paragraph at
 * once. The server now also pushes the text as it is produced, over the same SSE
 * connection but as frames that are never written to the log. This holds them
 * until the durable event catches up and takes over.
 */
export interface LiveReply {
  id: string;
  threadId: string;
  text: string;
  /** The model has stopped. The durable event lands within a poll cycle. */
  done: boolean;
  /** When the bubble appeared, for a stable timestamp while it grows. */
  ts: string;
  /**
   * Highest event seq seen when this bubble appeared.
   *
   * Retirement needs a watermark, not a text comparison: the durable event says
   * the same words, so matching on content would be both fragile and ambiguous
   * across repeated identical replies. Anything newer than this that answers the
   * same thread is this bubble's durable form.
   */
  sinceSeq: number;
}

/** Kinds that constitute the durable answer a live bubble stands in for. */
const ANSWER_KINDS = new Set<string>(
  CONVERSATION_KINDS.filter((kind) => kind !== "user.message"),
);

const replies = new SvelteMap<string, LiveReply>();
/**
 * Bubble id -> seq of the durable event that replaces it, once announced.
 *
 * Retiring on the announcement alone left a hole: the SSE frame arrives a full
 * round trip before the refetch it triggers, so the finished reply vanished and
 * the pending spinner came back for the length of that trip (25ms on loopback,
 * 438ms measured at 200ms RTT) before the durable bubble appeared in its place.
 * The list shrank and regrew, and the live-edge pin scrolled it twice. Holding
 * the bubble here until the replacement is actually rendered closes the hole by
 * construction, at any latency, and if the refetch fails it simply stays up.
 */
const retiring = new SvelteMap<string, number>();
let lastSeq = 0;

/** Highest seq among events the caller has rendered. The retirement gate. */
export function maxSeq(events: readonly { seq: number }[]): number {
  let highest = 0;
  for (const event of events) if (event.seq > highest) highest = event.seq;
  return highest;
}

/**
 * Replies for `threadId` that still have to stand in for themselves.
 *
 * `renderedSeq` is what the caller has on screen — pass `maxSeq` of the list the
 * bubbles are rendered alongside, so a reply steps aside in the same pass that
 * shows its durable form rather than some frames before it.
 */
export function liveRepliesFor(threadId: string, renderedSeq: number): LiveReply[] {
  return [...replies.values()].filter((reply) => {
    if (reply.threadId !== threadId) return false;
    const replacement = retiring.get(reply.id);
    return replacement === undefined || replacement > renderedSeq;
  });
}

interface Frame {
  id?: unknown;
  threadId?: unknown;
  delta?: unknown;
  text?: unknown;
  done?: unknown;
}

/** Merge one `stream` SSE frame. Malformed frames are dropped, not thrown: a
 *  bad frame must not take down the live connection that carries real events. */
export function applyLiveFrame(raw: string): void {
  let frame: Frame;
  try {
    frame = JSON.parse(raw) as Frame;
  } catch {
    return;
  }
  const { id, threadId } = frame;
  if (typeof id !== "string" || typeof threadId !== "string") return;
  const existing = replies.get(id);

  if (frame.done === true) {
    if (existing) replies.set(id, { ...existing, done: true });
    return;
  }
  // A new bubble on this thread is proof the previous one's durable event was
  // announced, so its held copy can go. Without this the map would keep every
  // finished reply, text and all, for the life of the page.
  if (!existing) {
    for (const [old, reply] of replies) {
      if (reply.threadId === threadId && retiring.has(old)) {
        replies.delete(old);
        retiring.delete(old);
      }
    }
  }
  // An absolute `text` is the replay a client gets when it connects mid-reply;
  // it replaces rather than appends, or the overlap would be duplicated.
  if (typeof frame.text === "string") {
    replies.set(id, {
      id,
      threadId,
      text: frame.text,
      done: false,
      ts: existing?.ts ?? new Date().toISOString(),
      sinceSeq: existing?.sinceSeq ?? lastSeq,
    });
    return;
  }
  if (typeof frame.delta !== "string" || frame.delta === "") return;
  replies.set(
    id,
    existing
      ? { ...existing, text: existing.text + frame.delta }
      : {
          id,
          threadId,
          text: frame.delta,
          done: false,
          ts: new Date().toISOString(),
          sinceSeq: lastSeq,
        },
  );
}

/**
 * Advance the watermark and mark whatever this event replaces.
 *
 * Called for every event arriving live, so the watermark reflects what the
 * client has actually seen rather than what it happens to have fetched. The
 * mark is not a removal: see `retiring`.
 */
export function noteLiveEvent(event: {
  seq: number;
  kind: string;
  threadId: string | null;
}): void {
  if (Number.isFinite(event.seq) && event.seq > lastSeq) lastSeq = event.seq;
  if (!ANSWER_KINDS.has(event.kind) || event.threadId === null) return;
  for (const [id, reply] of replies) {
    if (reply.threadId === event.threadId && event.seq > reply.sinceSeq) {
      retiring.set(id, event.seq);
    }
  }
}

/** Drop everything — sign-out, and a clean slate between tests. */
export function resetLiveText(): void {
  replies.clear();
  retiring.clear();
  lastSeq = 0;
}
