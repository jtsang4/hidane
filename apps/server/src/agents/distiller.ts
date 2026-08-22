import { commitCursor, nextBatch, appendEvent, type HidaneEvent } from "../kernel/events.js";
import {
  globalMemoryPath,
  parseMemories,
  promoteToFile,
  readMemoryFile,
  workItemMemoryPath,
  MEMORY_KINDS,
  type MemoryKind,
} from "../kernel/memories.js";
import { getWorkItem } from "../kernel/workItems.js";
import { config, sessionsDir } from "../config.js";
import { getRoleSession, promptRole } from "./sdk.js";
import { extractJson } from "./pi.js";

const CONSUMER = "distiller";
const PROMOTE_THRESHOLD = 0.8;

export const DISTILLER_CHARTER = `
You are the memory distiller of hidane, a persistent personal agent runtime.
You read recent events (messages, replies, execution outcomes) and extract only
DURABLE memories worth recalling in future sessions:
- stable facts about the user or their environment
- explicit user preferences ("prefer X", "never do Y")
- decisions made and their reasons
- reusable lessons from execution successes/failures
NOT ephemeral task state, NOT things already in the existing-memories list.

Respond with ONLY JSON:
{"memories":[{"kind":"fact|preference|decision|lesson","scope":"global|work_item","work_item_id":null,"content":"<one concise sentence in the user's language>","confidence":0.0}]}
Use an empty array when nothing durable appeared. Confidence reflects how
certain you are this should persist (>=0.8 means promote without review).
`.trim();

/** Kinds that can carry durable material; connector noise never reaches the model. */
export function meaningfulEvents(events: HidaneEvent[]): HidaneEvent[] {
  const KINDS = new Set([
    "user.message",
    "agent.reply",
    "escalation",
    "execution.finished",
    "work_item.created",
    "work_item.status_changed",
  ]);
  return events.filter((e) => KINDS.has(e.kind));
}

function eventLine(e: HidaneEvent): string {
  const text = e.payload["text"] ?? e.payload["summary"] ?? e.payload["note"];
  const body =
    typeof text === "string" ? text.slice(0, 500) : JSON.stringify(e.payload).slice(0, 200);
  return `[${e.kind}${e.workItemId ? ` ${e.workItemId}` : ""}] ${body}`;
}

interface DistilledMemory {
  kind: MemoryKind;
  scope: "global" | "work_item";
  work_item_id?: string | null;
  content: string;
  confidence: number;
}

export interface DistillResult {
  scanned: number;
  meaningful: number;
  extracted: number;
  promoted: number;
  skipped: boolean;
}

/**
 * Log consumer: batch events → extract candidate memories → high-confidence
 * ones land in the layered memory files. Cursor advances only when the batch
 * was processed (or contained nothing meaningful), so sparse material
 * accumulates instead of getting lost.
 */
export async function runDistillation(
  opts: { minEvents?: number } = {},
): Promise<DistillResult> {
  const minEvents = opts.minEvents ?? 10;
  const batch = await nextBatch(CONSUMER, 200);
  if (batch.length === 0) {
    return { scanned: 0, meaningful: 0, extracted: 0, promoted: 0, skipped: true };
  }
  const meaningful = meaningfulEvents(batch);
  const last = batch[batch.length - 1]!.seq;

  if (meaningful.length === 0) {
    await commitCursor(CONSUMER, last);
    return { scanned: batch.length, meaningful: 0, extracted: 0, promoted: 0, skipped: true };
  }
  if (meaningful.length < minEvents) {
    return {
      scanned: batch.length,
      meaningful: meaningful.length,
      extracted: 0,
      promoted: 0,
      skipped: true,
    };
  }

  const existing = parseMemories(await readMemoryFile(globalMemoryPath()));
  const existingText =
    existing.length > 0
      ? `Existing memories (do not duplicate):\n${existing.map((m) => `- ${m.content}`).join("\n")}`
      : "";

  const session = await getRoleSession("distiller", {
    charter: DISTILLER_CHARTER,
    cwd: config.home,
    sessionDir: sessionsDir(),
    thinking: config.routeThinking,
  });
  const run = await promptRole(
    session,
    [existingText, `Recent events:\n${meaningful.map(eventLine).join("\n")}`]
      .filter(Boolean)
      .join("\n\n"),
    config.routeTimeoutSec,
  );

  if (!run.ok) {
    await appendEvent({
      source: "agent:distiller",
      kind: "distill.run",
      payload: { ok: false, error: run.error ?? "unknown", scanned: batch.length },
    });
    // Do not advance the cursor: retry the same material next round.
    return {
      scanned: batch.length,
      meaningful: meaningful.length,
      extracted: 0,
      promoted: 0,
      skipped: true,
    };
  }

  const parsed = extractJson<{ memories?: DistilledMemory[] }>(run.text);
  const items = Array.isArray(parsed?.memories) ? parsed.memories : [];

  let promoted = 0;
  for (const item of items) {
    if (!item || typeof item.content !== "string" || item.content.trim() === "") continue;
    const kind = MEMORY_KINDS.includes(item.kind) ? item.kind : "fact";
    const scope = item.scope === "work_item" ? "work_item" : "global";
    const workItemId = item.work_item_id ?? undefined;
    const confidence = Math.max(0, Math.min(1, Number(item.confidence ?? 0)));
    const content = item.content.trim();

    await appendEvent({
      source: "agent:distiller",
      kind: "memory.candidate",
      workItemId: scope === "work_item" ? workItemId : undefined,
      payload: { kind, scope, content, confidence },
    });

    if (confidence >= PROMOTE_THRESHOLD) {
      let workspace: string | undefined;
      if (scope === "work_item" && workItemId) {
        workspace = (await getWorkItem(workItemId).catch(() => undefined))?.workspace;
      }
      await promoteToFile(
        { kind, content },
        workspace ? "work_item" : "global",
        { workspace, workItemId },
        "agent:distiller",
      );
      promoted++;
    }
  }

  await commitCursor(CONSUMER, last);
  await appendEvent({
    source: "agent:distiller",
    kind: "distill.run",
    payload: {
      ok: true,
      scanned: batch.length,
      meaningful: meaningful.length,
      extracted: items.length,
      promoted,
      durationMs: run.durationMs,
    },
  });
  return {
    scanned: batch.length,
    meaningful: meaningful.length,
    extracted: items.length,
    promoted,
    skipped: false,
  };
}

const RECALL_CAP = 4000;

/** Global memory file rendered for the Primary; cheap cross-day recall. */
export async function recallForPrimary(): Promise<string> {
  const text = (await readMemoryFile(globalMemoryPath())).trim();
  return text ? text.slice(0, RECALL_CAP) : "";
}

/** Work-item memory file + global memory for a Manager. */
export async function recallForManager(workItemId: string): Promise<string> {
  const item = await getWorkItem(workItemId).catch(() => undefined);
  const scoped = item
    ? (await readMemoryFile(workItemMemoryPath(item.workspace))).trim()
    : "";
  const global = (await readMemoryFile(globalMemoryPath())).trim();
  return [scoped, global].filter(Boolean).join("\n\n").slice(0, RECALL_CAP);
}
