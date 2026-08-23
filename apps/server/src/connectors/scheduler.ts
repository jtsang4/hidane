import { appendEvent } from "../kernel/events.js";
import { dueSchedules, markRun, type Schedule } from "../kernel/schedules.js";
import { handleUserMessage } from "../agents/primary.js";
import { deliverToMain } from "./feishu.js";

/**
 * Executor for user-defined schedules: the active counterpart of the passive
 * webhook connector. Firing is two-phase like every side effect — a
 * schedule.fired intent first, then the captured result — so a crash between
 * the two leaves evidence instead of a mystery.
 */

const HTTP_TIMEOUT_MS = 30_000;
/** Captured response bodies are evidence, not archives — the log is not a cache. */
const CAPTURE_LIMIT = 2_000;

async function fireHttp(schedule: Schedule): Promise<string> {
  const { url, method, headers, body, wake } = schedule.spec;
  const started = Date.now();
  try {
    const res = await fetch(url!, {
      method: method ?? "GET",
      headers: headers ?? {},
      ...(body !== undefined && method !== "GET" && method !== undefined
        ? { body }
        : {}),
      signal: AbortSignal.timeout(HTTP_TIMEOUT_MS),
      redirect: "follow",
    });
    const text = (await res.text()).slice(0, CAPTURE_LIMIT);
    await appendEvent({
      source: "connector:schedule",
      kind: "connector.http",
      payload: {
        scheduleId: schedule.id,
        name: schedule.name,
        url,
        status: res.status,
        ok: res.ok,
        durationMs: Date.now() - started,
        body: text,
        // Triage hint, declared in the definition, decided at triage time.
        wake: wake === true,
      },
    });
    return `http ${res.status}`;
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    await appendEvent({
      source: "connector:schedule",
      kind: "connector.http",
      payload: {
        scheduleId: schedule.id,
        name: schedule.name,
        url,
        status: 0,
        ok: false,
        durationMs: Date.now() - started,
        error: reason,
        wake: wake === true,
      },
    });
    return `error: ${reason}`;
  }
}

async function firePrompt(schedule: Schedule): Promise<string> {
  const outcome = await handleUserMessage(
    schedule.spec.prompt ?? "",
    `connector:schedule:${schedule.id}`,
  );
  // A scheduled reminder that only lands in the log reminds nobody: mirror the
  // reply onto the bound Feishu main chat when one exists (best-effort).
  if (outcome.reply) {
    await deliverToMain(`⏰ ${schedule.name}\n${outcome.reply}`).catch(() => {});
  }
  return `${outcome.action}${outcome.workItemId ? ` → ${outcome.workItemId}` : ""}`;
}

/** Fire one schedule now (loop tick or the API's run-now), with bookkeeping. */
export async function fireSchedule(schedule: Schedule): Promise<string> {
  await appendEvent({
    source: "connector:schedule",
    kind: "schedule.fired",
    payload: { scheduleId: schedule.id, name: schedule.name, action: schedule.action },
  });
  let status: string;
  try {
    status = schedule.action === "http" ? await fireHttp(schedule) : await firePrompt(schedule);
  } catch (err) {
    status = `error: ${err instanceof Error ? err.message : String(err)}`;
    await appendEvent({
      source: "connector:schedule",
      kind: "agent.error",
      payload: { scheduleId: schedule.id, error: status },
    });
  }
  await markRun(schedule.id, status);
  return status;
}

/**
 * Due-schedule loop. Sequential on purpose: schedules are rare and a prompt
 * firing runs an LLM chain — parallel firings would race the Primary session.
 */
export function startScheduler(tickSec = 15): () => void {
  let running = false;
  const tick = async () => {
    if (running) return; // a slow prompt chain must not stack ticks
    running = true;
    try {
      for (const schedule of await dueSchedules()) {
        await fireSchedule(schedule).catch((err) =>
          console.error(`schedule ${schedule.id} failed:`, err),
        );
      }
    } catch (err) {
      console.error("scheduler tick failed:", err);
    } finally {
      running = false;
    }
  };
  const handle = setInterval(() => void tick(), tickSec * 1000);
  void tick();
  return () => clearInterval(handle);
}
