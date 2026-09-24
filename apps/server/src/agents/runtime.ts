import { config } from "../config.js";
import { appendEvent } from "../kernel/events.js";
import { Runtime } from "../kernel/runtime.js";
import { overdueWorkItems, setWorkItemDeadline } from "../kernel/workItems.js";
import { archiveDay } from "../projections/archive.js";
import { today } from "../projections/worklog.js";
import { PRIMARY, MANAGER_PREFIX } from "./addresses.js";
import { primaryTurn } from "./primary.js";
import { managerTurn } from "./manager.js";
import { runDistillation } from "./distiller.js";
import { cancelTree, pumpExecutions } from "./workerPool.js";

/**
 * Deadlines are one of the three cancel sources (person, deadline, budget):
 * an overdue item is stopped with everything under it, and the person is told.
 */
async function enforceDeadlines(): Promise<void> {
  for (const item of await overdueWorkItems()) {
    await setWorkItemDeadline(item.id, null, "kernel:runtime");
    const stopped = await cancelTree(item.id, "deadline passed", "kernel:runtime");
    await appendEvent({
      source: "kernel:runtime",
      kind: "escalation",
      threadId: "main",
      workItemId: item.id,
      payload: {
        reason: "deadline",
        question: `「${item.title}」已到截止时间${stopped.length > 0 ? "，正在进行的执行已停止" : ""}。需要继续的话，直接回复这个任务。`,
      },
    });
  }
}

/** The daemon's event loop with every agent role and housekeeping task wired in. */
export function createAgentRuntime(): Runtime {
  const runtime = new Runtime({ maxConcurrentTurns: config.maxConcurrentTurns });
  runtime.register(PRIMARY, primaryTurn);
  runtime.register(MANAGER_PREFIX, managerTurn);
  runtime.registerTimer("worker-pump", () => pumpExecutions(), 2000);
  runtime.registerTimer("deadlines", enforceDeadlines, 30_000);
  runtime.registerIdle("distill", () => runDistillation({ minEvents: 10 }), {
    minIntervalMs: config.distillIntervalSec * 1000,
    maxIntervalMs: config.distillIntervalSec * 3000,
  });
  runtime.registerIdle("archive", () => archiveDay(today()), {
    minIntervalMs: 3600_000,
    maxIntervalMs: 2 * 3600_000,
  });
  return runtime;
}

let current: Runtime | undefined;

/** Set by the process that runs the loop; undefined in API-only or CLI processes. */
export function setCurrentRuntime(runtime: Runtime | undefined): void {
  current = runtime;
}

export function runtimeStatus(): { activeTurns: string[] } | undefined {
  return current?.status();
}
