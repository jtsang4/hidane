import { sql } from "./db.js";
import { genId } from "./ids.js";
import { appendEvent } from "./events.js";
import { ensureWorkspace } from "./workspaces.js";

export interface WorkItem {
  id: string;
  title: string;
  status: "open" | "done" | "closed";
  workspace: string;
  threadId: string;
  createdAt: string;
  updatedAt: string;
}

interface WorkItemRow {
  id: string;
  title: string;
  status: string;
  workspace: string;
  thread_id: string;
  created_at: Date;
  updated_at: Date;
}

function toWorkItem(row: WorkItemRow): WorkItem {
  return {
    id: row.id,
    title: row.title,
    status: row.status as WorkItem["status"],
    workspace: row.workspace,
    threadId: row.thread_id,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}

/**
 * Create a work item with its thread and workspace, and record the fact.
 * The thread is the interaction lane; the workspace is the execution home.
 */
export async function createWorkItem(
  title: string,
  source = "kernel",
): Promise<WorkItem> {
  const db = sql();
  const id = genId("wi", 6);
  const threadId = genId("th", 6);
  const workspace = await ensureWorkspace(id);
  await db`INSERT INTO threads (id, work_item_id, kind) VALUES (${threadId}, ${id}, 'work')`;
  await db`
    INSERT INTO work_items (id, title, workspace, thread_id)
    VALUES (${id}, ${title}, ${workspace}, ${threadId})`;
  const item = await getWorkItem(id);
  await appendEvent({
    source,
    kind: "work_item.created",
    threadId,
    workItemId: id,
    payload: { title, workspace },
  });
  return item;
}

export async function getWorkItem(id: string): Promise<WorkItem> {
  const db = sql();
  const rows = await db`SELECT * FROM work_items WHERE id = ${id}`;
  if (rows.length === 0) throw new Error(`work item not found: ${id}`);
  return toWorkItem(rows[0] as unknown as WorkItemRow);
}

export async function listWorkItems(
  status?: WorkItem["status"],
): Promise<WorkItem[]> {
  const db = sql();
  const rows = status
    ? await db`SELECT * FROM work_items WHERE status = ${status} ORDER BY created_at ASC`
    : await db`SELECT * FROM work_items ORDER BY created_at ASC`;
  return (rows as unknown as WorkItemRow[]).map(toWorkItem);
}

/** State transitions are facts: recorded to the log on every change. */
export async function setWorkItemStatus(
  id: string,
  status: WorkItem["status"],
  source = "kernel",
): Promise<WorkItem> {
  const db = sql();
  const item = await getWorkItem(id);
  await db`UPDATE work_items SET status = ${status}, updated_at = now() WHERE id = ${id}`;
  await appendEvent({
    source,
    kind: "work_item.status_changed",
    threadId: item.threadId,
    workItemId: id,
    payload: { from: item.status, to: status },
  });
  return getWorkItem(id);
}
