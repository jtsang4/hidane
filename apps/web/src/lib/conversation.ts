import type { HidaneEvent } from "./api.js";

/**
 * The conversation as the person reads it: each thing they said, with every
 * answer to it underneath — not in arrival order. A reply that lands minutes
 * later, after other messages, still shows up under the message it answers,
 * because every answer names its root.
 */
export interface Turn {
  /** Id of the message this turn is about (usually a main-thread user.message). */
  root: string;
  /** The person's message, when the root is one and it is loaded. */
  message: HidaneEvent | null;
  /** Non-person roots (a webhook, a schedule) carry their own label. */
  origin: { kind: "external" | "scheduled" | "unknown"; text: string } | null;
  /** Where the message went: the newest attribution wins. */
  attribution: HidaneEvent | null;
  /** "Which task is this?" — open until an attribution follows it. */
  ambiguous: HidaneEvent | null;
  /** Work item created for this message; its card is anchored here. */
  createdItem: string | null;
  /** Replies, questions, errors and steer notes, in order. */
  answers: HidaneEvent[];
  /** Position in the conversation (the root's seq, or the first sign of it). */
  seq: number;
  /** Most recent activity in the turn. */
  lastSeq: number;
}

const ANSWER_KINDS = new Set(["agent.reply", "escalation", "agent.error", "execution.steered"]);

function str(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

/** The message an event is about, if it says. */
export function rootIdOf(event: HidaneEvent): string | undefined {
  return str(event.payload["root"]) ?? str(event.payload["of"]);
}

/**
 * Group conversation events into turns.
 *
 * Events written before answers carried a root (the log is append-only, so
 * history keeps its old shape) attach to the closest earlier message, which is
 * how the old linear view read them.
 */
export function buildTurns(events: HidaneEvent[]): Turn[] {
  const ordered = [...events].sort((a, b) => a.seq - b.seq);
  const turns = new Map<string, Turn>();
  let lastMessageRoot: string | undefined;

  const turnFor = (root: string, seq: number): Turn => {
    let turn = turns.get(root);
    if (!turn) {
      turn = {
        root,
        message: null,
        origin: null,
        attribution: null,
        ambiguous: null,
        createdItem: null,
        answers: [],
        seq,
        lastSeq: seq,
      };
      turns.set(root, turn);
    }
    turn.lastSeq = Math.max(turn.lastSeq, seq);
    return turn;
  };

  for (const e of ordered) {
    if (e.kind === "user.message" && e.threadId === "main") {
      const turn = turnFor(e.id, e.seq);
      turn.message = e;
      turn.seq = Math.min(turn.seq, e.seq);
      lastMessageRoot = e.id;
      continue;
    }
    if (e.kind === "message.attributed") {
      const of = str(e.payload["of"]);
      if (!of) continue;
      const turn = turnFor(of, e.seq);
      turn.attribution = e;
      if (e.payload["created"] === true && e.workItemId) turn.createdItem = e.workItemId;
      continue;
    }
    if (e.kind === "attribution.ambiguous") {
      const of = str(e.payload["of"]);
      if (!of) continue;
      turnFor(of, e.seq).ambiguous = e;
      continue;
    }
    if (!ANSWER_KINDS.has(e.kind)) continue;
    // A subtask reports to its parent; the parent's summary is the answer.
    if (e.payload["child"] === true) continue;
    // Older runtimes wrote a Manager's answer twice — in the item's thread and
    // mirrored onto the main thread. Without a root, the thread copy is the twin.
    if (e.threadId !== "main" && !rootIdOf(e)) continue;
    const root = rootIdOf(e) ?? lastMessageRoot;
    if (!root) {
      turnFor(e.id, e.seq).answers.push(e);
      continue;
    }
    const turn = turnFor(root, e.seq);
    turn.answers.push(e);
    if (!turn.message && !turn.origin) {
      const kind = str(e.payload["rootKind"]);
      const text = str(e.payload["rootText"]);
      if (kind === "external" || kind === "scheduled") turn.origin = { kind, text: text ?? "" };
    }
  }

  const list = [...turns.values()];
  for (const turn of list) {
    if (!turn.message && !turn.origin) turn.origin = { kind: "unknown", text: "" };
    // An answer to "which one?" closes the question.
    if (turn.ambiguous && turn.attribution && turn.attribution.seq > turn.ambiguous.seq) {
      turn.ambiguous = null;
    }
  }
  return list.sort((a, b) => a.seq - b.seq);
}

/**
 * Is this turn still waiting for its first sign of life? Once the message is
 * attributed, the task card carries the progress; until then the turn itself
 * shows that routing is under way.
 */
export function turnRouting(turn: Turn): boolean {
  return (
    turn.message !== null &&
    turn.attribution === null &&
    turn.ambiguous === null &&
    turn.answers.length === 0
  );
}

/** Work item a turn belongs to now, if any. */
export function turnWorkItem(turn: Turn): string | null {
  return turn.attribution?.workItemId ?? null;
}
