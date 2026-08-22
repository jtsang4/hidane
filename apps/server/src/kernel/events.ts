import { sql } from "./db.js";
import { genId } from "./ids.js";

export interface EventInput {
  source: string;
  kind: string;
  threadId?: string | undefined;
  workItemId?: string | undefined;
  executionId?: string | undefined;
  payload?: Record<string, unknown> | undefined;
}

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
  };
}

const SELECT_COLS = sqlFragment();
function sqlFragment() {
  return `seq::int AS seq, id, ts, source, kind, thread_id, work_item_id, execution_id, payload`;
}

/** Append one event to the log (write-through; facts only). */
export async function appendEvent(input: EventInput): Promise<HidaneEvent> {
  const db = sql();
  const id = genId("ev", 10);
  const rows = await db`
    INSERT INTO events (id, source, kind, thread_id, work_item_id, execution_id, payload)
    VALUES (${id}, ${input.source}, ${input.kind}, ${input.threadId ?? null},
            ${input.workItemId ?? null}, ${input.executionId ?? null},
            ${db.json(JSON.parse(JSON.stringify(input.payload ?? {})) as never)})
    RETURNING seq::int AS seq, id, ts, source, kind, thread_id, work_item_id, execution_id, payload`;
  return toEvent(rows[0] as unknown as EventRow);
}

export interface ListFilter {
  threadId?: string | undefined;
  workItemId?: string | undefined;
  kind?: string | undefined;
  afterSeq?: number | undefined;
  /** Exclusive upper bound — used with `tail` to page backwards. */
  beforeSeq?: number | undefined;
  /** ISO date `YYYY-MM-DD` interpreted in the local timezone. */
  day?: string | undefined;
  tail?: number | undefined;
  limit?: number | undefined;
}

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
  if (filter.kind) add("kind = ?", filter.kind);
  if (filter.afterSeq !== undefined) add("seq > ?", filter.afterSeq);
  if (filter.beforeSeq !== undefined) add("seq < ?", filter.beforeSeq);
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
