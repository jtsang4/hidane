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
    // third arg: attached images (empty for a plain text message)
    expect(handleUserMessage).toHaveBeenCalledWith("hello", "connector:web", []);
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

  it("forwards chat images to the vision model and bounds what it accepts", async () => {
    const app = buildApp();
    const png = "iVBORw0KGgo=";
    const res = await app.request("/api/chat", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        text: "what is this",
        images: [
          { data: png, mimeType: "image/png" },
          { data: png, mimeType: "application/pdf" }, // not an image: dropped
          { data: png }, // malformed: dropped
        ],
      }),
    });
    expect(res.status).toBe(202);
    expect(handleUserMessage).toHaveBeenCalledWith("what is this", "connector:web", [
      { data: png, mimeType: "image/png" },
    ]);

    vi.clearAllMocks();
    // Images alone are a valid message; the model still needs words to route on.
    const imageOnly = await app.request("/api/chat", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ images: [{ data: png, mimeType: "image/jpeg" }] }),
    });
    expect(imageOnly.status).toBe(202);
    const call = (handleUserMessage as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(String(call[0])).toContain("图片");
    expect(call[2]).toHaveLength(1);

    vi.clearAllMocks();
    // Neither text nor images is still a bad request.
    const nothing = await app.request("/api/chat", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ images: [] }),
    });
    expect(nothing.status).toBe(400);
    expect(handleUserMessage).not.toHaveBeenCalled();
  });

  it("reports the worklog event count so an empty day is distinguishable", async () => {
    const app = buildApp();
    const empty = (await (await app.request("/api/worklog/2020-01-01")).json()) as {
      markdown: string;
      eventCount: number;
    };
    // The markdown is never empty — it always renders a heading.
    expect(empty.markdown.length).toBeGreaterThan(0);
    expect(empty.eventCount).toBe(0);

    await appendEvent({ source: "s", kind: "worklog.count.test", payload: {} });
    const busy = (await (await app.request("/api/worklog/today")).json()) as {
      eventCount: number;
    };
    expect(busy.eventCount).toBeGreaterThan(0);
  });

  it("creates a work item directly, optionally dispatching a first brief", async () => {
    const app = buildApp();
    const plain = await app.request("/api/work-items", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title: "手工建的工作项" }),
    });
    expect(plain.status).toBe(201);
    const created = (await plain.json()) as {
      item: { id: string; title: string; status: string };
      dispatched: boolean;
    };
    expect(created.item.title).toBe("手工建的工作项");
    expect(created.item.status).toBe("open");
    // No brief means nothing is dispatched — an empty item is a valid outcome.
    expect(created.dispatched).toBe(false);
    expect(handleThreadMessage).not.toHaveBeenCalled();

    const withBrief = await app.request("/api/work-items", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title: "带首条指令", brief: "先做第一步" }),
    });
    const dispatched = (await withBrief.json()) as { item: { id: string } };
    await new Promise((r) => setTimeout(r, 50));
    expect(handleThreadMessage).toHaveBeenCalledWith(dispatched.item.id, "先做第一步");
    // The brief is recorded before dispatch, so a crash mid-run leaves evidence.
    const recorded = await listEvents({ workItemId: dispatched.item.id, kind: "user.message" });
    expect(recorded).toHaveLength(1);

    const bad = await app.request("/api/work-items", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title: "   " }),
    });
    expect(bad.status).toBe(400);
  });

  it("archives a work item as closed, distinct from done", async () => {
    const item = await createWorkItem("archivable", "test");
    const app = buildApp();
    const res = await app.request(`/api/work-items/${item.id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ status: "closed" }),
    });
    expect(res.status).toBe(200);
    expect(((await res.json()) as { item: { status: string } }).item.status).toBe("closed");
    // Archived items drop out of the default list but survive in the log.
    const open = (await (await app.request("/api/work-items")).json()) as {
      items: { id: string }[];
    };
    expect(open.items.map((i) => i.id)).not.toContain(item.id);
    const all = (await (await app.request("/api/work-items?all")).json()) as {
      items: { id: string }[];
    };
    expect(all.items.map((i) => i.id)).toContain(item.id);
  });

  it("changes work item status and records the transition as a fact", async () => {
    const item = await createWorkItem("closable", "test");
    const app = buildApp();

    const res = await app.request(`/api/work-items/${item.id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ status: "done" }),
    });
    expect(res.status).toBe(200);
    expect(((await res.json()) as { item: { status: string } }).item.status).toBe("done");

    const changes = await listEvents({ kind: "work_item.status_changed" });
    expect(changes).toHaveLength(1);
    expect(changes[0]!.payload).toMatchObject({ from: "open", to: "done" });

    // Reopening is the same path, so the UI toggle cannot strand an item.
    const reopened = await app.request(`/api/work-items/${item.id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ status: "open" }),
    });
    expect(((await reopened.json()) as { item: { status: string } }).item.status).toBe("open");

    // Bad input is rejected before it can write an unknown status.
    const bad = await app.request(`/api/work-items/${item.id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ status: "banana" }),
    });
    expect(bad.status).toBe(400);
    expect((await listEvents({ kind: "work_item.status_changed" })).length).toBe(2);

    const missing = await app.request("/api/work-items/wi_nope", {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ status: "done" }),
    });
    expect(missing.status).toBe(404);
  });

  it("paginates events backwards with a seq cursor and reports hasMore", async () => {
    for (let i = 0; i < 12; i++) {
      await appendEvent({ source: "s", kind: "page.test", payload: { i } });
    }
    const app = buildApp();

    const first = (await (
      await app.request("/api/events?page=1&kind=page.test&limit=5")
    ).json()) as { events: { seq: number }[]; hasMore: boolean; oldestSeq: number };
    expect(first.events).toHaveLength(5);
    expect(first.hasMore).toBe(true);
    // newest page: ascending order, ending at the newest event
    expect(first.events[4]!.seq).toBeGreaterThan(first.events[0]!.seq);
    expect(first.oldestSeq).toBe(first.events[0]!.seq);

    const older = (await (
      await app.request(
        `/api/events?page=1&kind=page.test&limit=5&before=${first.oldestSeq}`,
      )
    ).json()) as { events: { seq: number }[]; hasMore: boolean };
    expect(older.events).toHaveLength(5);
    // strictly older, no overlap with the first page
    expect(older.events.at(-1)!.seq).toBeLessThan(first.oldestSeq);

    const last = (await (
      await app.request(
        `/api/events?page=1&kind=page.test&limit=5&before=${older.events[0]!.seq}`,
      )
    ).json()) as { events: unknown[]; hasMore: boolean };
    expect(last.events).toHaveLength(2);
    expect(last.hasMore).toBe(false);
  });

  it("reports status with triage lag, model and worklog renders", async () => {
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

describe("memory and execution control", () => {
  it("accepts a manually written memory and marks its provenance", async () => {
    const app = buildApp();
    const res = await app.request("/api/memories", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ kind: "preference", content: "回复一律用中文" }),
    });
    expect(res.status).toBe(201);
    const { entry } = (await res.json()) as { entry: { id: string; kind: string } };
    expect(entry.kind).toBe("preference");

    const events = await listEvents({ kind: "memory.promoted" });
    expect(events).toHaveLength(1);
    // Distinguishable from a distiller decision.
    expect(events[0]!.payload["manual"]).toBe(true);
    expect(events[0]!.payload["memoryId"]).toBe(entry.id);

    // It shows up in the listing the UI reads.
    const listed = (await (await app.request("/api/memories")).json()) as {
      entries: { id: string }[];
    };
    expect(listed.entries.map((e) => e.id)).toContain(entry.id);

    const empty = await app.request("/api/memories", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ content: "   " }),
    });
    expect(empty.status).toBe(400);
  });

  it("cancelling with nothing running is a 409, not a silent success", async () => {
    const app = buildApp();
    const res = await app.request("/api/work-items/wi_idle/cancel", { method: "POST" });
    expect(res.status).toBe(409);
    expect(await listEvents({ kind: "execution.cancelled" })).toHaveLength(0);
  });
});
