import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../src/agents/primary.js", () => ({
  handleUserMessage: vi.fn(async () => ({ action: "reply", reply: "stub" })),
}));
vi.mock("../src/agents/manager.js", () => ({
  handleThreadMessage: vi.fn(async () => "stub"),
}));

import { buildApp } from "../src/connectors/http.js";
import { config } from "../src/config.js";
import { appendEvent, listEvents } from "../src/kernel/events.js";
import { createWorkItem } from "../src/kernel/workItems.js";
import { handleUserMessage } from "../src/agents/primary.js";
import { handleThreadMessage } from "../src/agents/manager.js";

afterEach(() => {
  config.apiToken = undefined;
  vi.clearAllMocks();
});

describe("api", () => {
  it("requires bearer token on /api/* when configured (query token allowed for SSE)", async () => {
    config.apiToken = "sekret";
    const app = buildApp();
    expect((await app.request("/api/status")).status).toBe(401);
    expect(
      (
        await app.request("/api/status", {
          headers: { authorization: "Bearer sekret" },
        })
      ).status,
    ).toBe(200);
    expect((await app.request("/api/status?token=sekret")).status).toBe(200);
  });

  it("lists events with filters", async () => {
    await appendEvent({ source: "s", kind: "a", threadId: "main", payload: {} });
    await appendEvent({ source: "s", kind: "b", payload: {} });
    const app = buildApp();
    const res = await app.request("/api/events?thread=main");
    const body = (await res.json()) as { events: unknown[] };
    expect(body.events).toHaveLength(1);
  });

  it("accepts chat asynchronously and fires the primary", async () => {
    const app = buildApp();
    const res = await app.request("/api/chat", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ text: "hello" }),
    });
    expect(res.status).toBe(202);
    expect(handleUserMessage).toHaveBeenCalledWith("hello", "connector:web");
    const empty = await app.request("/api/chat", {
      method: "POST",
      body: JSON.stringify({ text: " " }),
    });
    expect(empty.status).toBe(400);
  });

  it("records and dispatches thread messages", async () => {
    const item = await createWorkItem("Api goal");
    const app = buildApp();
    const res = await app.request(`/api/work-items/${item.id}/messages`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ text: "do it" }),
    });
    expect(res.status).toBe(202);
    expect(handleThreadMessage).toHaveBeenCalledWith(item.id, "do it");
    const recorded = await listEvents({ threadId: item.threadId, kind: "user.message" });
    expect(recorded).toHaveLength(1);

    const missing = await app.request("/api/work-items/wi_nope/messages", {
      method: "POST",
      body: JSON.stringify({ text: "x" }),
    });
    expect(missing.status).toBe(404);
  });

  it("serves work item detail with thread events and 404s unknown ids", async () => {
    const item = await createWorkItem("Detail goal");
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: "done" },
    });
    const app = buildApp();
    const res = await app.request(`/api/work-items/${item.id}`);
    const body = (await res.json()) as { item: { id: string }; events: unknown[] };
    expect(body.item.id).toBe(item.id);
    expect(body.events.length).toBeGreaterThanOrEqual(1);
    expect((await app.request("/api/work-items/wi_nope")).status).toBe(404);
  });

  it("reports status with triage lag and worklog renders", async () => {
    await appendEvent({
      source: "connector:timer",
      kind: "connector.heartbeat",
      payload: {},
    });
    const app = buildApp();
    const status = (await (await app.request("/api/status")).json()) as {
      latestSeq: number;
      triageLag: number;
      lastHeartbeatAt: string | null;
    };
    expect(status.latestSeq).toBeGreaterThan(0);
    expect(status.lastHeartbeatAt).not.toBeNull();

    const log = await app.request("/api/worklog/today");
    expect(log.status).toBe(200);
    const bad = await app.request("/api/worklog/not-a-day");
    expect(bad.status).toBe(400);
  });
});
