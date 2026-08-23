import { Cron } from "croner";
import { sql } from "./db.js";
import { genId } from "./ids.js";
import { appendEvent } from "./events.js";

/**
 * Schedules are the user-defined timer connectors: "poll this URL every 10
 * minutes", "at 17:00 tell the agent to remind me". Definitions live in a
 * small state table (the scheduler needs an atomic due-query, same as
 * cursors); every definition change and every firing is recorded to the log.
 *
 * Two actions, matching what a schedule may do:
 *   http   — fetch a URL and CAPTURE the response as a connector.http event.
 *            Connector semantics: capture, never judge. Whether the model is
 *            woken is a declared triage hint (`wake`), decided at triage time.
 *   prompt — send text down the Primary fast lane, exactly like a user
 *            message. Covers agent tasks and "run a script" (a worker runs
 *            scripts inside its workspace with the guard in place).
 */

export type ScheduleAction = "http" | "prompt";

export interface ScheduleSpec {
  /** http: request definition. */
  url?: string;
  method?: string;
  headers?: Record<string, string>;
  body?: string;
  /** http: should triage wake the Primary with the captured response? */
  wake?: boolean;
  /** prompt: text for the Primary fast lane. */
  prompt?: string;
}

export interface Schedule {
  id: string;
  name: string;
  action: ScheduleAction;
  spec: ScheduleSpec;
  cron: string | null;
  intervalSec: number | null;
  timezone: string | null;
  enabled: boolean;
  nextRunAt: string | null;
  lastRunAt: string | null;
  lastStatus: string | null;
  createdAt: string;
  updatedAt: string;
}

interface ScheduleRow {
  id: string;
  name: string;
  action: string;
  spec: ScheduleSpec;
  cron: string | null;
  interval_sec: number | null;
  timezone: string | null;
  enabled: boolean;
  next_run_at: Date | null;
  last_run_at: Date | null;
  last_status: string | null;
  created_at: Date;
  updated_at: Date;
}

function toSchedule(row: ScheduleRow): Schedule {
  return {
    id: row.id,
    name: row.name,
    action: row.action as ScheduleAction,
    spec: row.spec ?? {},
    cron: row.cron,
    intervalSec: row.interval_sec,
    timezone: row.timezone,
    enabled: row.enabled,
    nextRunAt: row.next_run_at?.toISOString() ?? null,
    lastRunAt: row.last_run_at?.toISOString() ?? null,
    lastStatus: row.last_status,
    createdAt: row.created_at.toISOString(),
    updatedAt: row.updated_at.toISOString(),
  };
}

/**
 * Next firing time. Cron parsing is croner's job, not ours — a hand-rolled
 * parser is exactly the "trust your own crypto" mistake in another costume.
 * Throws on an invalid expression so the API can reject at definition time.
 */
export function computeNextRun(
  def: {
    cron?: string | null | undefined;
    intervalSec?: number | null | undefined;
    timezone?: string | null | undefined;
  },
  from = new Date(),
): Date {
  if (def.cron) {
    const job = new Cron(def.cron, {
      ...(def.timezone ? { timezone: def.timezone } : {}),
      paused: true,
    });
    const next = job.nextRun(from);
    job.stop();
    if (!next) throw new Error(`cron expression never fires: ${def.cron}`);
    return next;
  }
  const sec = Number(def.intervalSec);
  if (!Number.isFinite(sec) || sec < 10) {
    throw new Error("intervalSec must be a number >= 10");
  }
  return new Date(from.getTime() + sec * 1000);
}

export interface ScheduleInput {
  name: string;
  action: ScheduleAction;
  spec: ScheduleSpec;
  cron?: string | undefined;
  intervalSec?: number | undefined;
  timezone?: string | undefined;
  enabled?: boolean | undefined;
}

/** Definition-time validation: broken schedules must fail loudly at creation. */
export function validateInput(input: ScheduleInput): string | null {
  if (!input.name?.trim()) return "name required";
  if (input.action !== "http" && input.action !== "prompt") {
    return "action must be http | prompt";
  }
  if (Boolean(input.cron) === Boolean(input.intervalSec)) {
    return "exactly one of cron / intervalSec is required";
  }
  if (input.action === "http") {
    const url = input.spec?.url ?? "";
    if (!/^https?:\/\//.test(url)) return "spec.url must be an http(s) URL";
  }
  if (input.action === "prompt" && !input.spec?.prompt?.trim()) {
    return "spec.prompt required";
  }
  try {
    computeNextRun(input);
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
  return null;
}

export async function createSchedule(
  input: ScheduleInput,
  source = "connector:web",
): Promise<Schedule> {
  const invalid = validateInput(input);
  if (invalid) throw new Error(invalid);
  const db = sql();
  const id = genId("sc", 6);
  const nextRun = computeNextRun(input);
  await db`
    INSERT INTO schedules (id, name, action, spec, cron, interval_sec, timezone, enabled, next_run_at)
    VALUES (${id}, ${input.name.trim()}, ${input.action}, ${db.json(input.spec as never)},
            ${input.cron ?? null}, ${input.intervalSec ?? null}, ${input.timezone ?? null},
            ${input.enabled ?? true}, ${nextRun})`;
  const schedule = await getSchedule(id);
  await appendEvent({
    source,
    kind: "schedule.created",
    payload: {
      scheduleId: id,
      name: schedule.name,
      action: schedule.action,
      cron: schedule.cron,
      intervalSec: schedule.intervalSec,
    },
  });
  return schedule;
}

export async function getSchedule(id: string): Promise<Schedule> {
  const db = sql();
  const rows = await db`SELECT * FROM schedules WHERE id = ${id}`;
  if (rows.length === 0) throw new Error(`schedule not found: ${id}`);
  return toSchedule(rows[0] as unknown as ScheduleRow);
}

export async function listSchedules(): Promise<Schedule[]> {
  const db = sql();
  const rows = await db`SELECT * FROM schedules ORDER BY created_at ASC`;
  return (rows as unknown as ScheduleRow[]).map(toSchedule);
}

export async function updateSchedule(
  id: string,
  patch: Partial<ScheduleInput>,
  source = "connector:web",
): Promise<Schedule> {
  const current = await getSchedule(id);
  const merged: ScheduleInput = {
    name: patch.name ?? current.name,
    action: patch.action ?? current.action,
    spec: patch.spec ?? current.spec,
    // An explicit null-ish patch clears the other timing field, so switching
    // between cron and interval is a single PATCH rather than two.
    cron: "cron" in patch ? (patch.cron ?? undefined) : (current.cron ?? undefined),
    intervalSec:
      "intervalSec" in patch ? (patch.intervalSec ?? undefined) : (current.intervalSec ?? undefined),
    timezone: "timezone" in patch ? (patch.timezone ?? undefined) : (current.timezone ?? undefined),
    enabled: patch.enabled ?? current.enabled,
  };
  const invalid = validateInput(merged);
  if (invalid) throw new Error(invalid);
  const db = sql();
  const nextRun = merged.enabled ? computeNextRun(merged) : null;
  await db`
    UPDATE schedules SET
      name = ${merged.name.trim()},
      action = ${merged.action},
      spec = ${db.json(merged.spec as never)},
      cron = ${merged.cron ?? null},
      interval_sec = ${merged.intervalSec ?? null},
      timezone = ${merged.timezone ?? null},
      enabled = ${merged.enabled ?? true},
      next_run_at = ${nextRun},
      updated_at = now()
    WHERE id = ${id}`;
  const schedule = await getSchedule(id);
  await appendEvent({
    source,
    kind: "schedule.updated",
    payload: { scheduleId: id, enabled: schedule.enabled, name: schedule.name },
  });
  return schedule;
}

export async function deleteSchedule(id: string, source = "connector:web"): Promise<void> {
  const schedule = await getSchedule(id); // throws on unknown id
  const db = sql();
  await db`DELETE FROM schedules WHERE id = ${id}`;
  await appendEvent({
    source,
    kind: "schedule.deleted",
    payload: { scheduleId: id, name: schedule.name },
  });
}

/** Schedules whose time has come. */
export async function dueSchedules(now = new Date()): Promise<Schedule[]> {
  const db = sql();
  const rows = await db`
    SELECT * FROM schedules
    WHERE enabled = true AND next_run_at IS NOT NULL AND next_run_at <= ${now}
    ORDER BY next_run_at ASC`;
  return (rows as unknown as ScheduleRow[]).map(toSchedule);
}

/**
 * When an interval schedule should next run, given when it was *due*.
 *
 * Computing from the firing time instead looks equivalent and is not: a firing
 * lands slightly after the tick that triggered it, so the next due time lands
 * slightly after the following tick — which therefore skips it. With a tick
 * period close to the interval, the schedule fires every other tick and its
 * real cadence is double what was asked for. Measured: a 15s schedule fired
 * every 30s. The same slippage accumulates for longer intervals as drift,
 * since each period is measured from a progressively later start.
 *
 * Anchoring to the due time keeps the cadence on-grid. Missed periods are
 * skipped rather than replayed, so downtime still costs one firing, not a
 * storm of backdated ones.
 */
export function nextAfterRun(
  schedule: Pick<Schedule, "cron" | "intervalSec" | "timezone" | "nextRunAt">,
  now = new Date(),
): Date {
  // croner already returns the next cron moment after `now`, on-grid by design.
  if (schedule.cron) return computeNextRun(schedule, now);
  const intervalMs = Number(schedule.intervalSec) * 1000;
  if (!Number.isFinite(intervalMs) || intervalMs <= 0) {
    return computeNextRun(schedule, now);
  }
  const anchor = schedule.nextRunAt ? new Date(schedule.nextRunAt).getTime() : now.getTime();
  let next = anchor + intervalMs;
  // Advance in whole periods past now: keeps the grid, skips missed slots.
  if (next <= now.getTime()) {
    const periods = Math.ceil((now.getTime() - anchor) / intervalMs);
    next = anchor + periods * intervalMs;
    if (next <= now.getTime()) next += intervalMs;
  }
  return new Date(next);
}

/**
 * Bookkeeping after a firing. Missed slots are skipped, never replayed — after
 * daemon downtime a schedule fires once, not in a storm.
 */
export async function markRun(id: string, status: string, now = new Date()): Promise<void> {
  const schedule = await getSchedule(id);
  const nextRun = schedule.enabled ? nextAfterRun(schedule, now) : null;
  const db = sql();
  await db`
    UPDATE schedules
    SET last_run_at = ${now}, last_status = ${status.slice(0, 200)},
        next_run_at = ${nextRun}, updated_at = now()
    WHERE id = ${id}`;
}
