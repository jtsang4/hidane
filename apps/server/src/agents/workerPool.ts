import { join } from "node:path";
import { config } from "../config.js";
import { appendEvent, listEvents, type HidaneEvent } from "../kernel/events.js";
import { genId } from "../kernel/ids.js";
import { post, rootOf } from "../kernel/mailbox.js";
import {
  activeExecutionFor,
  activeExecutions,
  createExecution,
  getExecution,
  setExecutionStatus,
  type Execution,
} from "../kernel/executions.js";
import { ancestors, getWorkItem, subtree, type WorkItem } from "../kernel/workItems.js";
import { globalPolicyPath, workspacePolicyPath } from "../kernel/policies.js";
import { WORKER_CHARTER } from "./charters.js";
import {
  blockedQuestion,
  cancelActiveWorker,
  runWorkerExecution,
  type WorkerRunResult,
} from "./rpcWorker.js";

/**
 * Worker executions are the runtime's I/O: dispatched from a turn that then
 * ends, run as isolated subprocesses, and reported back by posting
 * `execution.finished` to the owner's mailbox. Every execution has an owner, so
 * no outcome is ever left without someone to read it.
 *
 * Two limits shape the queue: at most `maxWorkers` subprocesses at once, and at
 * most one per work item — one workspace, one writer.
 */

interface Job {
  executionId: string;
  workItemId: string;
  owner: string;
  instructions: string;
  started: HidaneEvent;
  /** Words from the person that arrived while the job was still queued. */
  amendments: string[];
}

const jobs = new Map<string, Job>();
const running = new Set<string>();
let pumping: Promise<void> | undefined;
let pumpAgain = false;

/** Test seam: replace the subprocess with a stub. */
let runner: typeof runWorkerExecution = runWorkerExecution;
export function setWorkerRunner(fn: typeof runWorkerExecution): void {
  runner = fn;
}

export async function dispatchExecution(opts: {
  item: WorkItem;
  owner: string;
  instructions: string;
  expect?: string | undefined;
  causedBy: HidaneEvent;
}): Promise<HidaneEvent> {
  const executionId = genId("ex", 6);
  await createExecution(executionId, opts.item.id, opts.owner);
  const started = await appendEvent({
    source: "agent:manager",
    kind: "execution.started",
    threadId: opts.item.threadId,
    workItemId: opts.item.id,
    executionId,
    causedBy: opts.causedBy.id,
    hop: opts.causedBy.hop + 1,
    payload: {
      instructions: opts.instructions,
      expect: opts.expect ?? null,
      root: rootOf(opts.causedBy),
    },
  });
  jobs.set(executionId, {
    executionId,
    workItemId: opts.item.id,
    owner: opts.owner,
    instructions: opts.instructions,
    started,
    amendments: [],
  });
  pumpExecutions();
  return started;
}

/** Add the person's words to a job that has not started yet. */
export function amendQueued(workItemId: string, text: string): boolean {
  for (const job of jobs.values()) {
    if (job.workItemId === workItemId && !running.has(job.executionId)) {
      job.amendments.push(text);
      return true;
    }
  }
  return false;
}

export function pumpExecutions(): Promise<void> {
  if (pumping) {
    pumpAgain = true;
    return pumping;
  }
  pumping = (async () => {
    do {
      pumpAgain = false;
      const queued = [...jobs.values()].filter((j) => !running.has(j.executionId));
      const busyItems = new Set([...running].map((id) => jobs.get(id)?.workItemId));
      for (const job of queued) {
        if (running.size >= config.maxWorkers) break;
        if (busyItems.has(job.workItemId)) continue;
        busyItems.add(job.workItemId);
        running.add(job.executionId);
        void runJob(job);
      }
    } while (pumpAgain);
  })().finally(() => {
    pumping = undefined;
  });
  return pumping;
}

async function policyFilesFor(item: WorkItem): Promise<string[]> {
  const chain = (await ancestors(item)).reverse();
  return [
    globalPolicyPath(),
    ...chain.map((a) => workspacePolicyPath(a.workspace)),
    workspacePolicyPath(item.workspace),
  ];
}

async function runJob(job: Job): Promise<void> {
  let result: WorkerRunResult;
  let item: WorkItem | undefined;
  // Tool events are appended in order and all land before the outcome does;
  // fire-and-forget appends used to race execution.finished.
  let trail: Promise<unknown> = Promise.resolve();
  try {
    item = await getWorkItem(job.workItemId);
    const current = await getExecution(job.executionId);
    if (current?.status !== "queued") {
      // Cancelled between dispatch and start; cancelTree already reported it.
      jobs.delete(job.executionId);
      running.delete(job.executionId);
      void pumpExecutions();
      return;
    }
    await setExecutionStatus(job.executionId, "running");
    await appendEvent({
      source: "agent:worker",
      kind: "execution.running",
      threadId: item.threadId,
      workItemId: item.id,
      executionId: job.executionId,
      payload: {},
    });
    const instructions =
      job.amendments.length > 0
        ? `${job.instructions}\n\nThe person added, after this was planned:\n${job.amendments.map((a) => `- ${a}`).join("\n")}`
        : job.instructions;
    const threadId = item.threadId;
    const workItemId = item.id;
    result = await runner({
      instructions,
      cwd: item.workspace,
      charter: WORKER_CHARTER,
      sessionDir: join(item.workspace, ".hidane", "sessions"),
      workItemId,
      executionId: job.executionId,
      timeoutSec: config.workerTimeoutSec,
      policyFiles: await policyFilesFor(item),
      // Two-phase side-effect trail: intent before the tool acts, result after.
      onToolEvent: (e) => {
        trail = trail.then(() =>
          appendEvent({
            source: "agent:worker",
            kind: e.phase === "start" ? "side_effect.intent" : "side_effect.result",
            threadId,
            workItemId,
            executionId: job.executionId,
            payload: {
              tool: e.toolName,
              ...(e.phase === "start" ? { input: e.detail ?? "" } : { isError: e.isError ?? false }),
            },
          }).catch(() => {}),
        );
      },
    });
  } catch (err) {
    result = {
      ok: false,
      text: "",
      error: err instanceof Error ? err.message : String(err),
      durationMs: 0,
      toolCalls: 0,
    };
  }
  try {
    await trail;
    await finishJob(job, item, result);
  } finally {
    jobs.delete(job.executionId);
    running.delete(job.executionId);
    void pumpExecutions();
  }
}

async function finishJob(job: Job, item: WorkItem | undefined, run: WorkerRunResult): Promise<void> {
  await reportOutcome(
    { executionId: job.executionId, workItemId: job.workItemId, owner: job.owner, started: job.started },
    item,
    run,
  );
}

/** Record the outcome and deliver it to the owner — the one way an execution ends. */
async function reportOutcome(
  execution: { executionId: string; workItemId: string; owner: string; started?: HidaneEvent | undefined },
  item: WorkItem | undefined,
  run: WorkerRunResult & { lost?: boolean },
): Promise<void> {
  const status = run.lost ? "lost" : run.cancelled ? "cancelled" : run.ok ? "done" : "failed";
  await setExecutionStatus(execution.executionId, status);
  const blocked = run.ok ? blockedQuestion(run.text) : null;
  const root = execution.started ? { root: rootOf(execution.started) } : {};
  for (const block of run.policyBlocks ?? []) {
    const parsed = /^blocked by hidane policy (\S+): ([\s\S]*)$/.exec(block.reason);
    await appendEvent({
      source: "agent:worker",
      kind: "policy.blocked",
      threadId: item?.threadId,
      workItemId: execution.workItemId,
      executionId: execution.executionId,
      payload: {
        tool: block.tool,
        rule: parsed?.[1] ?? null,
        reason: parsed?.[2] ?? block.reason,
        ...root,
      },
    });
  }
  await post({
    source: run.lost ? "kernel:runtime" : "agent:worker",
    kind: "execution.finished",
    mailbox: execution.owner,
    lane: "normal",
    threadId: item?.threadId,
    workItemId: execution.workItemId,
    executionId: execution.executionId,
    causedBy: execution.started,
    payload: {
      ok: run.ok,
      durationMs: run.durationMs,
      toolCalls: run.toolCalls,
      summary: run.text.slice(0, 8000),
      error: run.error ?? null,
      cancelled: run.cancelled ?? false,
      ...(run.lost ? { lost: true } : {}),
      blocked,
      policyBlocks: run.policyBlocks ?? [],
      ...root,
    },
  });
}

async function startedEventOf(execution: Execution): Promise<HidaneEvent | undefined> {
  return (
    await listEvents({ kind: "execution.started", workItemId: execution.workItemId, tail: 50 })
  ).find((e) => e.executionId === execution.id);
}

/**
 * Executions recorded as active that this process does not hold were lost to a
 * restart. Their owners are told like any other outcome, so nothing waits on
 * them forever.
 */
export async function recoverExecutions(): Promise<number> {
  let lost = 0;
  for (const execution of await activeExecutions()) {
    if (jobs.has(execution.id)) continue;
    lost++;
    const item = await getWorkItem(execution.workItemId).catch(() => undefined);
    await reportOutcome(
      {
        executionId: execution.id,
        workItemId: execution.workItemId,
        owner: execution.owner,
        started: await startedEventOf(execution),
      },
      item,
      {
        ok: false,
        text: "",
        error: "lost: the runtime restarted while this execution was active",
        durationMs: 0,
        toolCalls: 0,
        lost: true,
      },
    );
  }
  return lost;
}

/**
 * Stop a work item and everything under it — AbortSignal-style, the cancel
 * flows down the tree. The reason says which source fired it: a person, a
 * deadline, or a spent budget.
 */
export async function cancelTree(
  workItemId: string,
  reason: string,
  source: string,
): Promise<string[]> {
  const cancelled: string[] = [];
  for (const node of await subtree(workItemId)) {
    const execution = await activeExecutionFor(node.id);
    if (!execution) continue;
    // Intent before the effect: otherwise the stop lands in the log after the
    // execution it stopped.
    await appendEvent({
      source,
      kind: "execution.cancelled",
      threadId: node.threadId,
      workItemId: node.id,
      executionId: execution.id,
      payload: { reason, ...(node.id !== workItemId ? { via: workItemId } : {}) },
    });
    if (running.has(execution.id)) {
      // The worker stops and its own completion reports the cancellation.
      await cancelActiveWorker(node.id);
    } else {
      const job = jobs.get(execution.id);
      jobs.delete(execution.id);
      await reportOutcome(
        {
          executionId: execution.id,
          workItemId: node.id,
          owner: execution.owner,
          started: job?.started ?? (await startedEventOf(execution)),
        },
        node,
        { ok: false, text: "", error: "cancelled", cancelled: true, durationMs: 0, toolCalls: 0 },
      );
    }
    cancelled.push(node.id);
  }
  return cancelled;
}

export function poolStatus(): { running: number; queued: number } {
  return { running: running.size, queued: jobs.size - running.size };
}
