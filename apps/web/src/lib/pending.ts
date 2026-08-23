import type { HidaneEvent } from "./api.js";

/**
 * Is the runtime still working on the last thing said to it?
 *
 * The write API is asynchronous by design: POST returns 202 and the answer
 * arrives later as events. Without this the UI looks dead for the seconds — or
 * minutes, when a worker runs — between the two, and users retype their message.
 *
 * Derived from the log rather than from local request state, so it survives a
 * reload and is correct for work started from Feishu or the CLI too.
 */
export interface PendingState {
  /** Something is in flight and the user is waiting on it. */
  active: boolean;
  /** ISO timestamp work started, for an elapsed-time readout. */
  since: string | null;
  /** `executing` once a worker is running — a much longer wait than routing. */
  phase: "routing" | "executing" | null;
}

const ANSWERS = new Set(["agent.reply", "agent.error"]);

export function pendingState(events: HidaneEvent[]): PendingState {
  const ordered = [...events].sort((a, b) => a.seq - b.seq);

  // An execution outranks routing: it is the longer, more informative wait.
  const running = new Map<string, HidaneEvent>();
  for (const e of ordered) {
    if (!e.executionId) continue;
    if (e.kind === "execution.started") running.set(e.executionId, e);
    else if (e.kind === "execution.finished") running.delete(e.executionId);
  }
  const oldestRunning = [...running.values()].sort((a, b) => a.seq - b.seq)[0];
  if (oldestRunning) {
    return { active: true, since: oldestRunning.ts, phase: "executing" };
  }

  // Otherwise: a user message with no answer after it is still being routed.
  let lastUser: HidaneEvent | undefined;
  for (const e of ordered) {
    if (e.kind === "user.message") lastUser = e;
    else if (ANSWERS.has(e.kind) && lastUser && e.seq > lastUser.seq) lastUser = undefined;
  }
  return lastUser
    ? { active: true, since: lastUser.ts, phase: "routing" }
    : { active: false, since: null, phase: null };
}

/** Work item ids with an execution started but not finished. */
export function runningWorkItems(events: HidaneEvent[]): Set<string> {
  const ordered = [...events].sort((a, b) => a.seq - b.seq);
  const byExecution = new Map<string, string>();
  for (const e of ordered) {
    if (!e.executionId) continue;
    if (e.kind === "execution.started" && e.workItemId) {
      byExecution.set(e.executionId, e.workItemId);
    } else if (e.kind === "execution.finished") {
      byExecution.delete(e.executionId);
    }
  }
  return new Set(byExecution.values());
}

/** Whole seconds since `iso`, floored at 0. */
export function elapsedSeconds(iso: string, now = Date.now()): number {
  return Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
}
