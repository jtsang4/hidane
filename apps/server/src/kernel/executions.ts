import { sql } from "./db.js";

export type ExecutionStatus = "queued" | "running" | "done" | "failed" | "cancelled" | "lost";

export const ACTIVE_STATUSES: readonly ExecutionStatus[] = ["queued", "running"];

export interface Execution {
  id: string;
  workItemId: string;
  /** Mailbox that receives the outcome — every execution has an awaiter. */
  owner: string;
  status: ExecutionStatus;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}

interface ExecutionRow {
  id: string;
  work_item_id: string;
  owner: string;
  status: string;
  created_at: Date;
  started_at: Date | null;
  finished_at: Date | null;
}

function toExecution(row: ExecutionRow): Execution {
  return {
    id: row.id,
    workItemId: row.work_item_id,
    owner: row.owner,
    status: row.status as ExecutionStatus,
    createdAt: row.created_at.toISOString(),
    startedAt: row.started_at?.toISOString() ?? null,
    finishedAt: row.finished_at?.toISOString() ?? null,
  };
}

export async function createExecution(
  id: string,
  workItemId: string,
  owner: string,
): Promise<Execution> {
  const rows = await sql()`
    INSERT INTO executions (id, work_item_id, owner) VALUES (${id}, ${workItemId}, ${owner})
    RETURNING *`;
  return toExecution(rows[0] as unknown as ExecutionRow);
}

export async function setExecutionStatus(id: string, status: ExecutionStatus): Promise<void> {
  const db = sql();
  if (status === "running") {
    await db`UPDATE executions SET status = ${status}, started_at = now() WHERE id = ${id}`;
  } else if (status === "queued") {
    await db`UPDATE executions SET status = ${status} WHERE id = ${id}`;
  } else {
    await db`UPDATE executions SET status = ${status}, finished_at = now() WHERE id = ${id}`;
  }
}

export async function getExecution(id: string): Promise<Execution | undefined> {
  const rows = await sql()`SELECT * FROM executions WHERE id = ${id}`;
  const row = (rows as unknown as ExecutionRow[])[0];
  return row ? toExecution(row) : undefined;
}

/** Queued or running executions, oldest first. */
export async function activeExecutions(): Promise<Execution[]> {
  const rows = await sql()`
    SELECT * FROM executions WHERE status IN ('queued', 'running') ORDER BY created_at ASC`;
  return (rows as unknown as ExecutionRow[]).map(toExecution);
}

export async function activeExecutionFor(workItemId: string): Promise<Execution | undefined> {
  const rows = await sql()`
    SELECT * FROM executions
    WHERE work_item_id = ${workItemId} AND status IN ('queued', 'running')
    ORDER BY created_at ASC LIMIT 1`;
  const row = (rows as unknown as ExecutionRow[])[0];
  return row ? toExecution(row) : undefined;
}

/** Work items that have a queued or running execution. */
export async function busyWorkItemIds(): Promise<string[]> {
  const rows = await sql()`
    SELECT DISTINCT work_item_id FROM executions WHERE status IN ('queued', 'running')`;
  return (rows as unknown as { work_item_id: string }[]).map((r) => r.work_item_id);
}

export async function countExecutions(workItemId: string): Promise<number> {
  const rows = await sql()`SELECT count(*)::int AS n FROM executions WHERE work_item_id = ${workItemId}`;
  return Number((rows[0] as { n: number }).n);
}
