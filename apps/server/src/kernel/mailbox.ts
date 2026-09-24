import { sql } from "./db.js";
import { config } from "../config.js";
import {
  appendEvent,
  getCursor,
  listEvents,
  type EventInput,
  type HidaneEvent,
} from "./events.js";

/**
 * A mailbox is a derived view of the log, not a table: the events addressed to
 * an agent loop, read from that loop's cursor. Nothing is copied; replaying a
 * mailbox is resetting its cursor.
 */
export function mailboxCursor(address: string): string {
  return `mailbox:${address}`;
}

/** The main-thread message a chain of events started from. */
export function rootOf(event: HidaneEvent): string {
  const root = event.payload["root"];
  return typeof root === "string" && root ? root : event.id;
}

export interface PostInput extends Omit<EventInput, "mailbox" | "causedBy" | "hop"> {
  mailbox: string;
  causedBy?: HidaneEvent | undefined;
}

/**
 * Deliver a message by appending it. Returns null when the causal hop budget
 * is spent: the chain stops and a person is told instead, so two agents that
 * keep waking each other cost a bounded amount.
 */
export async function post(input: PostInput): Promise<HidaneEvent | null> {
  const { causedBy, ...rest } = input;
  const hop = causedBy ? causedBy.hop + 1 : 0;
  if (hop > config.maxHops) {
    await appendEvent({
      source: "kernel:runtime",
      kind: "escalation",
      threadId: "main",
      workItemId: input.workItemId,
      causedBy: causedBy?.id,
      hop,
      payload: {
        reason: "budget",
        question: `这条因果链已经连续触发 ${hop} 次，已自动暂停。需要继续的话，直接回复这个任务。`,
        blockedKind: input.kind,
        mailbox: input.mailbox,
        ...(causedBy ? { root: rootOf(causedBy) } : {}),
      },
    });
    return null;
  }
  const event = await appendEvent({ ...rest, causedBy: causedBy?.id, hop });
  // Seed the cursor from the oldest message rather than this one: a concurrent
  // first post with a higher seq must not be able to skip ours.
  await sql()`
    INSERT INTO cursors (consumer, seq)
    SELECT ${mailboxCursor(input.mailbox)}, COALESCE(MIN(seq), ${event.seq}) - 1
    FROM events WHERE mailbox = ${input.mailbox}
    ON CONFLICT DO NOTHING`;
  return event;
}

/** Undelivered messages for one mailbox, oldest first. */
export async function pendingMessages(address: string, limit = 100): Promise<HidaneEvent[]> {
  const cursor = await getCursor(mailboxCursor(address));
  return listEvents({ mailbox: address, afterSeq: cursor, limit });
}

export interface PendingMailbox {
  address: string;
  urgent: boolean;
  count: number;
}

/** Mailboxes with undelivered messages; interrupt-lane first, then oldest first. */
export async function mailboxesWithPending(): Promise<PendingMailbox[]> {
  const rows = await sql()`
    SELECT substr(c.consumer, 9) AS address,
           bool_or(e.lane = 'interrupt') AS urgent,
           count(*)::int AS count,
           min(e.seq) AS oldest
    FROM cursors c
    JOIN events e ON e.mailbox = substr(c.consumer, 9) AND e.seq > c.seq
    WHERE c.consumer LIKE 'mailbox:%'
    GROUP BY c.consumer
    ORDER BY urgent DESC, oldest ASC`;
  return (rows as unknown as { address: string; urgent: boolean; count: number }[]).map((r) => ({
    address: r.address,
    urgent: r.urgent,
    count: Number(r.count),
  }));
}
