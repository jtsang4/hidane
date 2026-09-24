import { sql } from "../kernel/db.js";
import { activeExecutions } from "../kernel/executions.js";
import { mailboxesWithPending } from "../kernel/mailbox.js";
import { listWorkItems, type WorkItem } from "../kernel/workItems.js";

export type CardState =
  | "waiting"
  | "running"
  | "queued"
  | "thinking"
  | "delegated"
  | "idle"
  | "done"
  | "closed";

export interface BoardCard {
  item: WorkItem;
  state: CardState;
  /** The Manager's latest statement of what this work item is about. */
  understanding: string | null;
  lastReply: { text: string; ts: string; seq: number } | null;
  execution: {
    id: string;
    status: string;
    startedAt: string | null;
    instructions: string;
    toolCalls: number;
    lastTool: string | null;
  } | null;
  /** An open question for the person (unanswered escalation). */
  escalation: {
    id: string;
    question: string;
    reason: string;
    path: unknown[];
    ts: string;
  } | null;
  lastPolicyBlock: { reason: string; ts: string } | null;
  /** The message the card was created for — where it sits in the conversation. */
  anchor: string | null;
  childIds: string[];
  lastSeq: number;
}

interface LatestRow {
  seq: number;
  id: string;
  ts: Date;
  kind: string;
  work_item_id: string;
  execution_id: string | null;
  payload: Record<string, unknown>;
}

const RECENT_MS = 24 * 3600 * 1000;

/** A tool call in words a person scans: the command, or the file it touched. */
export function describeTool(payload: Record<string, unknown>): string {
  const tool = String(payload["tool"] ?? "");
  const raw = String(payload["input"] ?? "");
  let detail = raw;
  try {
    const input = JSON.parse(raw) as Record<string, unknown>;
    const pick = input["command"] ?? input["path"] ?? input["file_path"] ?? input["pattern"] ?? input["url"];
    if (typeof pick === "string") detail = pick;
  } catch {
    // The intent stores a truncated JSON string; fall back to what is there.
  }
  return `${tool} ${detail.replace(/\s+/g, " ").trim()}`.slice(0, 160);
}

/**
 * Task-card view: derived, read-only and rebuildable from the log and the
 * small state tables. Open items plus anything closed within the last day.
 */
export async function buildBoard(activeTurns: string[] = []): Promise<BoardCard[]> {
  const now = Date.now();
  const items = (await listWorkItems()).filter(
    (i) => i.status === "open" || now - Date.parse(i.updatedAt) < RECENT_MS,
  );
  if (items.length === 0) return [];
  const ids = items.map((i) => i.id);
  const db = sql();
  const latest = (await db`
    SELECT DISTINCT ON (work_item_id, kind)
      seq::int AS seq, id, ts, kind, work_item_id, execution_id, payload
    FROM events
    WHERE work_item_id = ANY(${ids})
      AND kind IN ('work_item.understanding', 'agent.reply', 'user.message',
                   'work_item.created', 'policy.blocked', 'side_effect.intent')
      OR (work_item_id = ANY(${ids}) AND kind = 'escalation' AND payload ? 'question')
    ORDER BY work_item_id, kind, seq DESC`) as unknown as LatestRow[];
  const maxSeq = (await db`
    SELECT work_item_id, max(seq)::int AS seq FROM events
    WHERE work_item_id = ANY(${ids}) GROUP BY work_item_id`) as unknown as {
    work_item_id: string;
    seq: number;
  }[];
  const executions = await activeExecutions();
  const pending = new Set((await mailboxesWithPending()).map((m) => m.address));
  const turns = new Set(activeTurns);

  const byItem = new Map<string, Map<string, LatestRow>>();
  for (const row of latest) {
    let m = byItem.get(row.work_item_id);
    if (!m) byItem.set(row.work_item_id, (m = new Map()));
    m.set(row.kind, row);
  }

  const cards: BoardCard[] = [];
  for (const item of items) {
    const rows = byItem.get(item.id) ?? new Map<string, LatestRow>();
    const escalationRow = rows.get("escalation");
    const lastUser = rows.get("user.message");
    // Only a question is waiting on the person. Older runtimes also wrote an
    // `escalation` as a "finished a run" marker (a `note`, no question).
    const openEscalation =
      escalationRow &&
      typeof escalationRow.payload["question"] === "string" &&
      (!lastUser || lastUser.seq < escalationRow.seq) &&
      item.status === "open"
        ? escalationRow
        : undefined;
    const execution = executions.find((e) => e.workItemId === item.id);
    let executionView: BoardCard["execution"] = null;
    if (execution) {
      const [started] = (await db`
        SELECT payload FROM events
        WHERE execution_id = ${execution.id} AND kind = 'execution.started' LIMIT 1`) as unknown as {
        payload: Record<string, unknown>;
      }[];
      const [count] = (await db`
        SELECT count(*)::int AS n FROM events
        WHERE execution_id = ${execution.id} AND kind = 'side_effect.intent'`) as unknown as {
        n: number;
      }[];
      const lastTool = rows.get("side_effect.intent");
      executionView = {
        id: execution.id,
        status: execution.status,
        startedAt: execution.startedAt,
        instructions: String(started?.payload["instructions"] ?? ""),
        toolCalls: Number(count?.n ?? 0),
        lastTool:
          lastTool && lastTool.execution_id === execution.id ? describeTool(lastTool.payload) : null,
      };
    }
    const address = `manager:${item.id}`;
    const state: CardState = openEscalation
      ? "waiting"
      : execution?.status === "running"
        ? "running"
        : execution
          ? "queued"
          : pending.has(address) || turns.has(address)
            ? "thinking"
            : item.status === "open" && items.some((c) => c.parentId === item.id && c.status === "open")
              ? "delegated"
              : item.status === "open"
                ? "idle"
                : item.status;
    const reply = rows.get("agent.reply");
    const block = rows.get("policy.blocked");
    const understanding = rows.get("work_item.understanding");
    const created = rows.get("work_item.created");
    cards.push({
      item,
      state,
      understanding: understanding ? String(understanding.payload["text"] ?? "") : null,
      lastReply: reply
        ? { text: String(reply.payload["text"] ?? "").slice(0, 1200), ts: reply.ts.toISOString(), seq: reply.seq }
        : null,
      execution: executionView,
      escalation: openEscalation
        ? {
            id: openEscalation.id,
            question: String(openEscalation.payload["question"] ?? ""),
            reason: String(openEscalation.payload["reason"] ?? "question"),
            path: Array.isArray(openEscalation.payload["path"]) ? openEscalation.payload["path"] : [],
            ts: openEscalation.ts.toISOString(),
          }
        : null,
      lastPolicyBlock: block
        ? { reason: String(block.payload["reason"] ?? ""), ts: block.ts.toISOString() }
        : null,
      anchor: typeof created?.payload["of"] === "string" ? created.payload["of"] : null,
      childIds: items.filter((c) => c.parentId === item.id).map((c) => c.id),
      lastSeq: maxSeq.find((r) => r.work_item_id === item.id)?.seq ?? 0,
    });
  }
  return cards;
}
