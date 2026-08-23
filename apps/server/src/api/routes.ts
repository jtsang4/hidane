import type { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import { appendEvent, getCursor, listEvents } from "../kernel/events.js";
import { getWorkItem, listWorkItems, setWorkItemStatus } from "../kernel/workItems.js";
import { renderDay, today } from "../projections/worklog.js";
import { handleUserMessage } from "../agents/primary.js";
import { handleThreadMessage } from "../agents/manager.js";
import { describeEffectiveModel } from "../agents/sdk.js";
import {
  forgetMemory,
  globalMemoryPath,
  parseMemories,
  readMemoryFile,
} from "../kernel/memories.js";

/** Interval between SSE keep-alives; clients treat prolonged silence as a
 *  dropped stream, so this bounds how long a stale view can look current. */
const SSE_PING_MS = 15_000;

/**
 * Read API = queries over the event log and its state tables.
 * Write API = two async entrances (main chat, thread message): they return
 * immediately after recording intent; replies arrive as events over SSE.
 */
export function registerApi(app: Hono): void {
  app.get("/api/events", async (c) => {
    const q = c.req.query();
    const filters = {
      threadId: q["thread"],
      workItemId: q["item"],
      kind: q["kind"],
      day: q["day"],
    };
    // Cursor pagination walks backwards through the log: `before` is exclusive,
    // matching the append-only spine's monotonic seq (same idea as consumer
    // cursors). One extra row tells us whether an older page exists.
    if (q["before"] !== undefined || q["page"] !== undefined) {
      const limit = Math.min(Number(q["limit"] ?? 50), 200);
      const before = q["before"] !== undefined ? Number(q["before"]) : undefined;
      const page = await listEvents({
        ...filters,
        beforeSeq: before,
        tail: limit + 1,
      });
      const hasMore = page.length > limit;
      const events = hasMore ? page.slice(page.length - limit) : page;
      return c.json({
        events,
        hasMore,
        oldestSeq: events[0]?.seq ?? null,
      });
    }
    const events = await listEvents({
      ...filters,
      afterSeq: q["after"] !== undefined ? Number(q["after"]) : undefined,
      tail: q["tail"] !== undefined ? Number(q["tail"]) : undefined,
      limit: q["limit"] !== undefined ? Number(q["limit"]) : undefined,
    });
    return c.json({ events });
  });

  app.get("/api/events/stream", (c) => {
    const after = Number(c.req.query("after") ?? Number.MAX_SAFE_INTEGER);
    return streamSSE(c, async (stream) => {
      let cursor = after;
      if (!Number.isFinite(cursor) || cursor === Number.MAX_SAFE_INTEGER) {
        const last = await listEvents({ tail: 1 });
        cursor = last[0]?.seq ?? 0;
      }
      let open = true;
      stream.onAbort(() => {
        open = false;
      });
      await stream.writeSSE({ event: "hello", data: JSON.stringify({ cursor }) });
      let lastWrite = Date.now();
      while (open) {
        const fresh = await listEvents({ afterSeq: cursor, limit: 100 });
        for (const event of fresh) {
          cursor = event.seq;
          await stream.writeSSE({
            event: "hidane",
            id: String(event.seq),
            data: JSON.stringify(event),
          });
          lastWrite = Date.now();
        }
        // Keep-alive. A dead server does not close the socket in a way the
        // browser reports: an open EventSource stays readyState OPEN forever
        // and fires no error, so clients can only detect the loss by silence.
        // This also stops idle proxies from dropping a quiet stream.
        if (Date.now() - lastWrite >= SSE_PING_MS) {
          await stream.writeSSE({ event: "ping", data: String(Date.now()) });
          lastWrite = Date.now();
        }
        await stream.sleep(1500);
      }
    });
  });

  app.get("/api/work-items", async (c) => {
    const all = c.req.query("all") !== undefined;
    const items = await listWorkItems(all ? undefined : "open");
    return c.json({ items });
  });

  app.get("/api/work-items/:id", async (c) => {
    try {
      const item = await getWorkItem(c.req.param("id"));
      const events = await listEvents({ threadId: item.threadId });
      return c.json({ item, events });
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
  });

  app.patch("/api/work-items/:id", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as { status?: string };
    const status = body.status;
    if (status !== "open" && status !== "done" && status !== "closed") {
      return c.json({ ok: false, error: "status must be open | done | closed" }, 400);
    }
    try {
      const item = await setWorkItemStatus(c.req.param("id"), status, "connector:web");
      return c.json({ ok: true, item });
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
  });

  app.post("/api/chat", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as { text?: string };
    const text = (body.text ?? "").trim();
    if (!text) return c.json({ ok: false, error: "text required" }, 400);
    // Fire and forget: the fast lane records + routes; outcome arrives as events.
    void handleUserMessage(text, "connector:web").catch(async (err) => {
      await appendEvent({
        source: "connector:web",
        kind: "agent.error",
        threadId: "main",
        payload: { error: String(err) },
      }).catch(() => {});
    });
    return c.json({ ok: true, accepted: true }, 202);
  });

  app.post("/api/work-items/:id/messages", async (c) => {
    const id = c.req.param("id");
    const body = (await c.req.json().catch(() => ({}))) as { text?: string };
    const text = (body.text ?? "").trim();
    if (!text) return c.json({ ok: false, error: "text required" }, 400);
    let item;
    try {
      item = await getWorkItem(id);
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
    await appendEvent({
      source: "connector:web",
      kind: "user.message",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text },
    });
    void handleThreadMessage(item.id, text).catch(async (err) => {
      await appendEvent({
        source: "connector:web",
        kind: "agent.error",
        threadId: item.threadId,
        workItemId: item.id,
        payload: { error: String(err) },
      }).catch(() => {});
    });
    return c.json({ ok: true, accepted: true }, 202);
  });

  app.get("/api/memories", async (c) => {
    const text = await readMemoryFile(globalMemoryPath());
    return c.json({ path: globalMemoryPath(), entries: parseMemories(text), markdown: text });
  });

  app.delete("/api/memories/:id", async (c) => {
    const ok = await forgetMemory(globalMemoryPath(), c.req.param("id"), "connector:web");
    return ok ? c.json({ ok: true }) : c.json({ ok: false, error: "not found" }, 404);
  });

  app.get("/api/worklog/:day", async (c) => {
    const day = c.req.param("day") === "today" ? today() : c.req.param("day");
    if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) {
      return c.json({ ok: false, error: "day must be YYYY-MM-DD" }, 400);
    }
    // The count is reported separately: an empty day still renders a heading,
    // so clients cannot tell "nothing happened" from the markdown alone.
    const [markdown, events] = await Promise.all([renderDay(day), listEvents({ day })]);
    return c.json({ day, markdown, eventCount: events.length });
  });

  app.get("/api/status", async (c) => {
    const [latest, heartbeats, cursor, open, model] = await Promise.all([
      listEvents({ tail: 1 }),
      listEvents({ kind: "connector.heartbeat", tail: 1 }),
      getCursor("triage"),
      listWorkItems("open"),
      describeEffectiveModel().catch((err) => `error: ${String(err.message ?? err)}`),
    ]);
    const latestSeq = latest[0]?.seq ?? 0;
    return c.json({
      latestSeq,
      triageCursor: cursor,
      triageLag: Math.max(0, latestSeq - cursor),
      lastHeartbeatAt: heartbeats[0]?.ts ?? null,
      openWorkItems: open.length,
      model,
    });
  });
}
