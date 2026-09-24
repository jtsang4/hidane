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
  /** Parent in the work tree; fan-out creates children, never parallel writers. */
  parentId: string | null;
  deadlineAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface WorkItemRow {
  id: string;
  title: string;
  status: string;
  workspace: string;
  thread_id: string;
  parent_id: string | null;
  deadline_at: Date | null;
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
    parentId: row.parent_id,
    deadlineAt: row.deadline_at?.toISOString() ?? null,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}

/**
 * Create a work item with its thread and workspace, and record the fact.
 * The thread is the interaction lane; the workspace is the execution home
 * (a plain dir, or a git worktree when the work targets a local repo).
 */
export async function createWorkItem(
  title: string,
  source = "kernel",
  opts: {
    repo?: string | undefined;
    parentId?: string | undefined;
    /** The message this work item was created for (its anchor in the conversation). */
    of?: string | undefined;
  } = {},
): Promise<WorkItem> {
  const db = sql();
  const id = genId("wi", 6);
  const threadId = genId("th", 6);
  const workspace = await ensureWorkspace(id, opts.repo);
  await db`INSERT INTO threads (id, work_item_id, kind) VALUES (${threadId}, ${id}, 'work')`;
  await db`
    INSERT INTO work_items (id, title, workspace, thread_id, parent_id)
    VALUES (${id}, ${title}, ${workspace.path}, ${threadId}, ${opts.parentId ?? null})`;
  const item = await getWorkItem(id);
  await appendEvent({
    source,
    kind: "work_item.created",
    threadId,
    workItemId: id,
    payload: {
      title,
      workspace: workspace.path,
      provider: workspace.provider,
      branch: workspace.branch ?? null,
      repo: opts.repo ?? null,
      workspaceError: workspace.error ?? null,
      parentId: opts.parentId ?? null,
      ...(opts.of ? { of: opts.of } : {}),
    },
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

export async function listChildren(parentId: string): Promise<WorkItem[]> {
  const rows = await sql()`
    SELECT * FROM work_items WHERE parent_id = ${parentId} ORDER BY created_at ASC`;
  return (rows as unknown as WorkItemRow[]).map(toWorkItem);
}

/** The item and every descendant, parents before children. */
export async function subtree(id: string): Promise<WorkItem[]> {
  const rows = await sql()`
    WITH RECURSIVE tree AS (
      SELECT *, 0 AS depth FROM work_items WHERE id = ${id}
      UNION ALL
      SELECT w.*, tree.depth + 1 FROM work_items w JOIN tree ON w.parent_id = tree.id
    )
    SELECT * FROM tree ORDER BY depth ASC, created_at ASC`;
  return (rows as unknown as WorkItemRow[]).map(toWorkItem);
}

/** Ancestors from the parent up to the root. */
export async function ancestors(item: WorkItem): Promise<WorkItem[]> {
  const chain: WorkItem[] = [];
  let parentId = item.parentId;
  while (parentId && chain.length < 32) {
    const parent = await getWorkItem(parentId);
    chain.push(parent);
    parentId = parent.parentId;
  }
  return chain;
}

export async function setWorkItemDeadline(
  id: string,
  deadlineAt: string | null,
  source = "kernel",
): Promise<WorkItem> {
  const item = await getWorkItem(id);
  await sql()`UPDATE work_items SET deadline_at = ${deadlineAt}, updated_at = now() WHERE id = ${id}`;
  await appendEvent({
    source,
    kind: "work_item.deadline_set",
    threadId: item.threadId,
    workItemId: id,
    payload: { deadlineAt },
  });
  return getWorkItem(id);
}

/** Open items whose deadline has passed. */
export async function overdueWorkItems(now = new Date()): Promise<WorkItem[]> {
  const rows = await sql()`
    SELECT * FROM work_items
    WHERE status = 'open' AND deadline_at IS NOT NULL AND deadline_at <= ${now}`;
  return (rows as unknown as WorkItemRow[]).map(toWorkItem);
}
