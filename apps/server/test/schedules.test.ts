import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../src/agents/primary.js", () => ({
  handleUserMessage: vi.fn(async () => ({ action: "reply", reply: "scheduled reply" })),
}));
vi.mock("../src/agents/manager.js", () => ({
  handleThreadMessage: vi.fn(async () => "stub"),
}));

import { buildApp } from "../src/connectors/http.js";
import { fireSchedule } from "../src/connectors/scheduler.js";
import { handleUserMessage } from "../src/agents/primary.js";
import { listEvents } from "../src/kernel/events.js";
import { triageEvent } from "../src/kernel/triage.js";
import {
  computeNextRun,
  createSchedule,
  dueSchedules,
  getSchedule,
  listSchedules,
  validateInput,
} from "../src/kernel/schedules.js";

afterEach(() => vi.clearAllMocks());

describe("schedule definitions", () => {
  it("computes the next run for intervals and cron, in the given timezone", () => {
    const from = new Date("2026-08-23T00:00:00Z");
    expect(computeNextRun({ intervalSec: 600 }, from).toISOString()).toBe(
      "2026-08-23T00:10:00.000Z",
    );
    // 17:00 Shanghai is 09:00 UTC.
    const next = computeNextRun(
      { cron: "0 17 * * *", timezone: "Asia/Shanghai" },
      from,
    );
    expect(next.toISOString()).toBe("2026-08-23T09:00:00.000Z");
  });

  it("rejects broken definitions at creation time, loudly", () => {
    expect(validateInput({ name: "", action: "http", spec: { url: "https://x" } })).toContain(
      "name",
    );
    expect(
      validateInput({ name: "x", action: "http", spec: { url: "https://x" } }),
    ).toContain("exactly one");
    expect(
      validateInput({
        name: "x",
        action: "http",
        spec: { url: "ftp://nope" },
        intervalSec: 60,
      }),
    ).toContain("http(s)");
    expect(
      validateInput({ name: "x", action: "prompt", spec: {}, intervalSec: 60 }),
    ).toContain("prompt");
    // Sub-10s intervals would let a typo hammer the runtime.
    expect(
      validateInput({ name: "x", action: "prompt", spec: { prompt: "p" }, intervalSec: 1 }),
    ).toContain(">= 10");
    expect(
      validateInput({ name: "x", action: "prompt", spec: { prompt: "p" }, cron: "not a cron" }),
    ).toBeTruthy();
    expect(
      validateInput({ name: "x", action: "prompt", spec: { prompt: "p" }, cron: "*/5 * * * *" }),
    ).toBeNull();
  });

  it("creates, lists and marks due; the definition change is a logged fact", async () => {
    const schedule = await createSchedule({
      name: "poll health",
      action: "http",
      spec: { url: "https://example.com/health" },
      intervalSec: 60,
    });
    expect((await listSchedules()).map((s) => s.id)).toContain(schedule.id);
    expect(schedule.nextRunAt).not.toBeNull();
    // Not due yet (next run is a minute out)…
    expect((await dueSchedules()).map((s) => s.id)).not.toContain(schedule.id);
    // …but due once the clock passes it.
    const later = new Date(Date.now() + 90_000);
    expect((await dueSchedules(later)).map((s) => s.id)).toContain(schedule.id);
    expect(await listEvents({ kind: "schedule.created" })).toHaveLength(1);
  });
});

describe("schedule firing", () => {
  it("prompt action goes down the Primary fast lane with the schedule as source", async () => {
    const schedule = await createSchedule({
      name: "daily reminder",
      action: "prompt",
      spec: { prompt: "提醒我喝水" },
      cron: "0 17 * * *",
    });
    const status = await fireSchedule(schedule);
    expect(status).toBe("reply");
    expect(handleUserMessage).toHaveBeenCalledWith(
      "提醒我喝水",
      `connector:schedule:${schedule.id}`,
    );
    // Two-phase: the firing intent is on the log, and bookkeeping advanced.
    expect(await listEvents({ kind: "schedule.fired" })).toHaveLength(1);
    const after = await getSchedule(schedule.id);
    expect(after.lastStatus).toBe("reply");
    expect(after.lastRunAt).not.toBeNull();
  });

  it("http action captures the response — success and failure alike", async () => {
    const okSchedule = await createSchedule({
      name: "poll",
      action: "http",
      spec: { url: "https://example.com/x", wake: true },
      intervalSec: 60,
    });
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response("pong", { status: 200 }));
    expect(await fireSchedule(okSchedule)).toBe("http 200");
    fetchMock.mockRejectedValueOnce(new Error("connect ECONNREFUSED"));
    expect(await fireSchedule(okSchedule)).toContain("ECONNREFUSED");
    fetchMock.mockRestore();

    const captured = await listEvents({ kind: "connector.http" });
    expect(captured).toHaveLength(2);
    expect(captured[0]!.payload).toMatchObject({ status: 200, ok: true, body: "pong", wake: true });
    // The failed poll is captured too — a dead endpoint is exactly the news.
    expect(captured[1]!.payload).toMatchObject({ ok: false, status: 0 });
    expect(String(captured[1]!.payload["error"])).toContain("ECONNREFUSED");
  });

  it("triage wakes the primary only when the definition says wake", () => {
    const base = {
      seq: 1, id: "e", ts: "", source: "connector:schedule",
      threadId: null, workItemId: null, executionId: null,
    };
    expect(
      triageEvent({ ...base, kind: "connector.http", payload: { wake: true } }).action,
    ).toBe("wake_primary");
    expect(
      triageEvent({ ...base, kind: "connector.http", payload: { wake: false } }).action,
    ).toBe("record");
    expect(
      triageEvent({ ...base, kind: "connector.http", payload: {} }).action,
    ).toBe("record");
  });
});

describe("schedule api", () => {
  it("CRUD round trip with validation and run-now", async () => {
    const app = buildApp();
    const created = await app.request("/api/schedules", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        name: "api schedule",
        action: "prompt",
        spec: { prompt: "hello" },
        intervalSec: 3600,
      }),
    });
    expect(created.status).toBe(201);
    const { schedule } = (await created.json()) as { schedule: { id: string } };

    const bad = await app.request("/api/schedules", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name: "broken", action: "prompt", spec: { prompt: "x" }, cron: "banana" }),
    });
    expect(bad.status).toBe(400);

    // Disable, then verify next_run_at is cleared so the loop skips it.
    const disabled = await app.request(`/api/schedules/${schedule.id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ enabled: false }),
    });
    expect(disabled.status).toBe(200);
    expect(
      ((await disabled.json()) as { schedule: { nextRunAt: string | null } }).schedule.nextRunAt,
    ).toBeNull();

    const ran = await app.request(`/api/schedules/${schedule.id}/run`, { method: "POST" });
    expect(ran.status).toBe(200);
    expect(((await ran.json()) as { status: string }).status).toBe("reply");

    expect(
      (await app.request(`/api/schedules/${schedule.id}`, { method: "DELETE" })).status,
    ).toBe(200);
    expect((await app.request("/api/schedules/sc_nope/run", { method: "POST" })).status).toBe(404);
    const listed = (await (await app.request("/api/schedules")).json()) as {
      schedules: { id: string }[];
    };
    expect(listed.schedules.map((s) => s.id)).not.toContain(schedule.id);
  });
});
