import { join } from "node:path";
import { appendEvent, listEvents } from "../kernel/events.js";
import { getWorkItem } from "../kernel/workItems.js";
import { genId } from "../kernel/ids.js";
import { config } from "../config.js";
import { extractJson } from "./pi.js";
import { MANAGER_CHARTER, WORKER_CHARTER } from "./charters.js";
import { getManagerSession, promptRole } from "./sdk.js";
import { hasActiveWorker, runWorkerExecution, steerActiveWorker } from "./rpcWorker.js";
import { recallForManager } from "./distiller.js";

interface ManagerDecision {
  instructions?: string | null;
  expect?: string;
  reply?: string;
}

/** One plan+execute cycle at a time per work item (no concurrent workers in one workspace). */
const itemLocks = new Map<string, Promise<unknown>>();

function withItemLock<T>(workItemId: string, fn: () => Promise<T>): Promise<T> {
  const prev = itemLocks.get(workItemId) ?? Promise.resolve();
  const run = prev.catch(() => {}).then(fn);
  itemLocks.set(workItemId, run);
  return run;
}

/**
 * Manager: default addressee of a work item's thread — a persistent SDK
 * session scoped to the work item. Plans one execution per incoming message
 * and spawns an isolated RPC worker in the work item's workspace.
 */
export async function handleThreadMessage(
  workItemId: string,
  message: string,
): Promise<string> {
  const item = await getWorkItem(workItemId);

  // Routing waterfall: a message during a running execution is steered into
  // it instead of spawning a second worker in the same workspace.
  if (hasActiveWorker(workItemId)) {
    const steered = await steerActiveWorker(workItemId, message);
    if (steered) {
      await appendEvent({
        source: "agent:manager",
        kind: "execution.steered",
        threadId: item.threadId,
        workItemId,
        payload: { text: message },
      });
      return "steered into the running execution";
    }
  }

  return withItemLock(workItemId, () => managerCycle(workItemId, message));
}

async function managerCycle(workItemId: string, message: string): Promise<string> {
  const item = await getWorkItem(workItemId);
  const sessionsRoot = join(item.workspace, ".hidane", "sessions");
  const history = await listEvents({ threadId: item.threadId, tail: 12 });
  const historyText = history
    .filter((e) => ["user.message", "agent.reply"].includes(e.kind))
    .map((e) => `[${e.kind}] ${String(e.payload["text"] ?? "")}`)
    .join("\n");

  const session = await getManagerSession(
    item.id,
    item.workspace,
    join(sessionsRoot, "manager"),
    MANAGER_CHARTER,
  );
  const memories = await recallForManager(workItemId);
  const planning = await promptRole(
    session,
    [
      memories,
      `Work item: ${item.id} — ${item.title} (status: ${item.status})`,
      `Workspace: ${item.workspace}`,
      historyText ? `Recent thread:\n${historyText}` : "",
      `New message:\n${message}`,
    ]
      .filter(Boolean)
      .join("\n\n"),
    config.routeTimeoutSec,
  );

  await appendEvent({
    source: "agent:manager",
    kind: "manager.decision",
    threadId: item.threadId,
    workItemId,
    payload: { ok: planning.ok, durationMs: planning.durationMs },
  });

  if (!planning.ok) {
    const text = `manager planning failed: ${planning.error ?? "unknown"}`;
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId,
      payload: { text },
    });
    return text;
  }

  const decision = extractJson<ManagerDecision>(planning.text);

  // No execution needed: manager answers in-thread directly.
  if (
    decision &&
    (decision.instructions === null || decision.instructions === undefined) &&
    decision.reply
  ) {
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId,
      payload: { text: decision.reply },
    });
    return decision.reply;
  }

  const instructions = decision?.instructions ?? message;
  const executionId = genId("ex", 6);

  await appendEvent({
    source: "agent:manager",
    kind: "execution.started",
    threadId: item.threadId,
    workItemId,
    executionId,
    payload: { instructions, expect: decision?.expect ?? null },
  });

  const run = await runWorkerExecution({
    instructions,
    cwd: item.workspace,
    charter: WORKER_CHARTER,
    sessionDir: sessionsRoot,
    workItemId,
    executionId,
    timeoutSec: config.workerTimeoutSec,
    // Two-phase side-effect trail: intent before the tool acts, result after.
    onToolEvent: (e) => {
      void appendEvent({
        source: "agent:worker",
        kind: e.phase === "start" ? "side_effect.intent" : "side_effect.result",
        threadId: item.threadId,
        workItemId,
        executionId,
        payload: {
          tool: e.toolName,
          ...(e.phase === "start"
            ? { input: e.detail ?? "" }
            : { isError: e.isError ?? false }),
        },
      });
    },
  });

  await appendEvent({
    source: "agent:worker",
    kind: "execution.finished",
    threadId: item.threadId,
    workItemId,
    executionId,
    payload: {
      ok: run.ok,
      durationMs: run.durationMs,
      toolCalls: run.toolCalls,
      summary: run.text.slice(0, 8000),
      error: run.error ?? null,
      cancelled: run.cancelled ?? false,
    },
  });

  const replyText = run.ok
    ? run.text
    : run.cancelled
      ? `执行已取消。${run.text ? `\n已完成的部分：\n${run.text}` : ""}`
      : `execution failed: ${run.error ?? "unknown"}\n${run.text}`;

  await appendEvent({
    source: "agent:manager",
    kind: "agent.reply",
    threadId: item.threadId,
    workItemId,
    executionId,
    payload: { text: replyText.slice(0, 8000) },
  });

  return replyText;
}
