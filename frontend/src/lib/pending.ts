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

// A delegated main-thread request also ends with an escalation marker. Keep
// this terminal for older events that were written before the Manager reply
// was mirrored back onto the main thread.
const ANSWERS = new Set(["agent.reply", "agent.error", "escalation"]);

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

/**
 * Let process-level truth override what a bounded window of events implies.
 *
 * A work item's events are now paged, and a busy run emits enough side effects
 * to push its own `execution.started` off the first page. `pendingState` would
 * then report `routing`, or nothing at all, while a worker is demonstrably
 * running — and the detail page hides its stop button on exactly that signal.
 * The server reports `running` from the worker registry, which no window can
 * truncate.
 */
export function withActiveWorker(state: PendingState, running: boolean): PendingState {
  if (!running || state.phase === "executing") return state;
  return { active: true, since: state.since, phase: "executing" };
}

/** Whole seconds since `iso`, floored at 0. */
export function elapsedSeconds(iso: string, now = Date.now()): number {
  return Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
}
