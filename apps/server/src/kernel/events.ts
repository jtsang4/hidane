import { sql } from "./db.js";
import { genId } from "./ids.js";

export interface EventInput {
  source: string;
  kind: string;
  threadId?: string | undefined;
  workItemId?: string | undefined;
  executionId?: string | undefined;
  payload?: Record<string, unknown> | undefined;
  /** Address of the agent loop this event is also a message to. */
  mailbox?: string | undefined;
  lane?: Lane | undefined;
  /** The event this one was caused by; drives the causal hop budget. */
  causedBy?: string | undefined;
  hop?: number | undefined;
}

/**
 * Delivery priority for mailbox messages. `interrupt` (a person talking) is
 * taken before `normal` (continuations, connector wakes); `idle` work only runs
 * when no mailbox has anything pending.
 */
export type Lane = "interrupt" | "normal" | "idle";

export interface HidaneEvent {
  seq: number;
  id: string;
  ts: string;
  source: string;
  kind: string;
  threadId: string | null;
  workItemId: string | null;
  executionId: string | null;
  payload: Record<string, unknown>;
  mailbox: string | null;
  lane: Lane | null;
  causedBy: string | null;
  hop: number;
}

interface EventRow {
  seq: number;
  id: string;
  ts: Date;
  source: string;
  kind: string;
  thread_id: string | null;
  work_item_id: string | null;
  execution_id: string | null;
  payload: Record<string, unknown>;
  mailbox: string | null;
  lane: string | null;
  caused_by: string | null;
  hop: number;
}

function toEvent(row: EventRow): HidaneEvent {
  return {
    seq: Number(row.seq),
    id: row.id,
    ts: row.ts.toISOString(),
    source: row.source,
    kind: row.kind,
    threadId: row.thread_id,
    workItemId: row.work_item_id,
    executionId: row.execution_id,
    payload: row.payload ?? {},
    mailbox: row.mailbox,
    lane: row.lane as Lane | null,
    causedBy: row.caused_by,
    hop: Number(row.hop ?? 0),
  };
}

const SELECT_COLS = sqlFragment();
function sqlFragment() {
  return `seq::int AS seq, id, ts, source, kind, thread_id, work_item_id, execution_id, payload, mailbox, lane, caused_by, hop`;
}

/** Append one event to the log (write-through; facts only). */
export async function appendEvent(input: EventInput): Promise<HidaneEvent> {
  const db = sql();
  const id = genId("ev", 10);
  const rows = await db`
    INSERT INTO events (id, source, kind, thread_id, work_item_id, execution_id, payload,
                        mailbox, lane, caused_by, hop)
    VALUES (${id}, ${input.source}, ${input.kind}, ${input.threadId ?? null},
            ${input.workItemId ?? null}, ${input.executionId ?? null},
            ${db.json(JSON.parse(JSON.stringify(input.payload ?? {})) as never)},
            ${input.mailbox ?? null}, ${input.mailbox ? (input.lane ?? "normal") : null},
            ${input.causedBy ?? null}, ${input.hop ?? 0})
    RETURNING seq::int AS seq, id, ts, source, kind, thread_id, work_item_id, execution_id,
              payload, mailbox, lane, caused_by, hop`;
  return toEvent(rows[0] as unknown as EventRow);
}

/** One event by id, or undefined. */
export async function getEvent(id: string): Promise<HidaneEvent | undefined> {
  const db = sql();
  const rows = await db.unsafe(`SELECT ${SELECT_COLS} FROM events WHERE id = $1`, [id]);
  const row = (rows as unknown as EventRow[])[0];
  return row ? toEvent(row) : undefined;
}

export interface ListFilter {
  threadId?: string | undefined;
  workItemId?: string | undefined;
  mailbox?: string | undefined;
  kind?: string | undefined;
  /**
   * Several kinds, OR'd. Paging a chat needs this: with the kind test applied
   * after the fetch, a page of N rows yields an unpredictable number of
   * bubbles, and a page that happens to hold only unrendered kinds adds
   * nothing at all — which reads as "load older is broken".
   */
  kinds?: readonly string[] | undefined;
  afterSeq?: number | undefined;
  /** Exclusive upper bound — used with `tail` to page backwards. */
  beforeSeq?: number | undefined;
  /** ISO date `YYYY-MM-DD` interpreted in the local timezone. */
  day?: string | undefined;
  /**
   * Match a top-level payload key, e.g. `{ scheduleId: "sc_x" }`.
   *
   * Needed because some things are identified only inside the payload — a
   * schedule's firings, say. Filtering those by scanning a tail window silently
   * loses history the moment the window is shorter than the gap between runs.
   */
  payloadEquals?: { key: string; value: string } | undefined;
  /**
   * The conversation view: everything said on the main thread plus every
   * answer, whichever thread it was written on. Answers are grouped under the
   * message they answer (payload.root) by the reader, not by position.
   */
  conversation?: boolean | undefined;
  tail?: number | undefined;
  limit?: number | undefined;
}

export const CONVERSATION_MAIN_KINDS = [
  "user.message",
  "agent.reply",
  "agent.error",
  "escalation",
  "message.attributed",
  "attribution.ambiguous",
] as const;
export const CONVERSATION_ANY_KINDS = ["agent.reply", "execution.steered"] as const;

/** Read events in seq order with optional filters. */
export async function listEvents(filter: ListFilter = {}): Promise<HidaneEvent[]> {
  const db = sql();
  const where: string[] = [];
  const params: unknown[] = [];
  const add = (clause: string, value: unknown) => {
    params.push(value);
    where.push(clause.replace("?", `$${params.length}`));
  };
  if (filter.threadId) add("thread_id = ?", filter.threadId);
  if (filter.workItemId) add("work_item_id = ?", filter.workItemId);
  if (filter.mailbox) add("mailbox = ?", filter.mailbox);
  if (filter.kind) add("kind = ?", filter.kind);
  if (filter.kinds && filter.kinds.length > 0) {
    // One placeholder per kind rather than `= ANY($n)`: this query runs through
    // `db.unsafe`, where postgres.js infers parameter types from the JS value,
    // and array serialisation is not worth betting a query shape on.
    const holes = filter.kinds.map((kind) => {
      params.push(kind);
      return `$${params.length}`;
    });
    where.push(`kind IN (${holes.join(", ")})`);
  }
  if (filter.conversation) {
    const main = CONVERSATION_MAIN_KINDS.map((k) => `'${k}'`).join(", ");
    const any = CONVERSATION_ANY_KINDS.map((k) => `'${k}'`).join(", ");
    where.push(`((thread_id = 'main' AND kind IN (${main})) OR kind IN (${any}))`);
  }
  if (filter.afterSeq !== undefined) add("seq > ?", filter.afterSeq);
  if (filter.beforeSeq !== undefined) add("seq < ?", filter.beforeSeq);
  if (filter.payloadEquals) {
    params.push(filter.payloadEquals.key);
    const keyParam = `$${params.length}`;
    params.push(filter.payloadEquals.value);
    where.push(`payload->>${keyParam} = $${params.length}`);
  }
  if (filter.day) {
    const start = new Date(`${filter.day}T00:00:00`);
    const end = new Date(start.getTime() + 24 * 3600 * 1000);
    add("ts >= ?", start.toISOString());
    add("ts < ?", end.toISOString());
  }
  const whereSql = where.length > 0 ? `WHERE ${where.join(" AND ")}` : "";
  if (filter.tail !== undefined) {
    const rows = await db.unsafe(
      `SELECT ${SELECT_COLS} FROM events ${whereSql} ORDER BY seq DESC LIMIT ${Number(filter.tail)}`,
      params as never[],
    );
    return (rows as unknown as EventRow[]).map(toEvent).reverse();
  }
  const limit = filter.limit !== undefined ? `LIMIT ${Number(filter.limit)}` : "";
  const rows = await db.unsafe(
    `SELECT ${SELECT_COLS} FROM events ${whereSql} ORDER BY seq ASC ${limit}`,
    params as never[],
  );
  return (rows as unknown as EventRow[]).map(toEvent);
}

/** Get a consumer's committed cursor (0 when absent). */
export async function getCursor(consumer: string): Promise<number> {
  const db = sql();
  const rows = await db`SELECT seq::int AS seq FROM cursors WHERE consumer = ${consumer}`;
  return rows.length > 0 ? Number((rows[0] as { seq: number }).seq) : 0;
}

/** Commit a consumer's cursor position. */
export async function commitCursor(consumer: string, seq: number): Promise<void> {
  const db = sql();
  await db`
    INSERT INTO cursors (consumer, seq, updated_at) VALUES (${consumer}, ${seq}, now())
    ON CONFLICT (consumer) DO UPDATE SET seq = ${seq}, updated_at = now()`;
}

/** Reset a consumer's cursor (replay support). */
export async function resetCursor(consumer: string, seq = 0): Promise<void> {
  await commitCursor(consumer, seq);
}

/** Fetch the next uncommitted batch for a consumer. Caller commits after handling. */
export async function nextBatch(
  consumer: string,
  limit = 50,
): Promise<HidaneEvent[]> {
  const after = await getCursor(consumer);
  return listEvents({ afterSeq: after, limit });
}
