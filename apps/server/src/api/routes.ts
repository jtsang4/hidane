import type { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import { appendEvent, getCursor, listEvents } from "../kernel/events.js";
import {
  createWorkItem,
  getWorkItem,
  listWorkItems,
  setWorkItemStatus,
} from "../kernel/workItems.js";
import { renderDay, today } from "../projections/worklog.js";
import { handleUserMessage } from "../agents/primary.js";
import { handleThreadMessage } from "../agents/manager.js";
import { describeEffectiveModel } from "../agents/sdk.js";
import { IMAGE_ONLY_TEXT } from "../connectors/feishu.js";
import {
  appendMemory,
  forgetMemory,
  globalMemoryPath,
  parseMemories,
  readMemoryFile,
  MEMORY_KINDS,
  type MemoryKind,
} from "../kernel/memories.js";
import {
  createSchedule,
  deleteSchedule,
  getSchedule,
  listSchedules,
  updateSchedule,
  type ScheduleInput,
} from "../kernel/schedules.js";
import { fireSchedule } from "../connectors/scheduler.js";
import { activeExecutionId, cancelActiveWorker, hasActiveWorker } from "../agents/rpcWorker.js";
import {
  listArtifacts,
  readArtifact,
  resolveInside,
} from "../kernel/artifacts.js";
import { createReadStream } from "node:fs";

/** The web channel feeds the same vision model as Feishu, so its uploads go
 *  through the same shape. Bounded here: base64 rides in the JSON body, and an
 *  unbounded one would be a trivial way to exhaust memory. */
const MAX_IMAGES = 4;
const MAX_IMAGE_BYTES = 6 * 1024 * 1024;

function parseInboundImages(
  raw: { data?: unknown; mimeType?: unknown }[] | undefined,
): { data: string; mimeType: string }[] {
  if (!Array.isArray(raw)) return [];
  const images: { data: string; mimeType: string }[] = [];
  for (const item of raw.slice(0, MAX_IMAGES)) {
    const { data, mimeType } = item ?? {};
    if (typeof data !== "string" || typeof mimeType !== "string") continue;
    if (!mimeType.startsWith("image/")) continue;
    // base64 inflates by 4/3; compare against the decoded size.
    if (data.length * 0.75 > MAX_IMAGE_BYTES) continue;
    images.push({ data, mimeType });
  }
  return images;
}

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
      let open = true;
      stream.onAbort(() => {
        open = false;
      });
      // Greet before touching the database. The tail lookup used to run first,
      // and when it failed under connection pressure the response was already
      // committed with 200 headers and no body at all — one connection in eight
      // under concurrent load. A client cannot distinguish that from a hang.
      await stream.writeSSE({ event: "hello", data: JSON.stringify({ after }) });
      let lastWrite = Date.now();
      let cursor = after;
      while (open) {
        try {
          if (!Number.isFinite(cursor) || cursor === Number.MAX_SAFE_INTEGER) {
            const last = await listEvents({ tail: 1 });
            cursor = last[0]?.seq ?? 0;
          }
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
        } catch (err) {
          // A transient query failure must not silently end the stream: keep
          // the connection and let the next tick retry. The client's own
          // staleness check still catches a genuinely dead server.
          console.error("sse poll failed:", err);
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
    // Which items are busy is process-level truth here. Clients used to infer
    // it from a window of recent events, which quietly went wrong once a busy
    // run pushed its own execution.started out of that window.
    const running = items.filter((i) => hasActiveWorker(i.id)).map((i) => i.id);
    return c.json({ items, running });
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

  // Not every task starts as a conversation: sometimes you already know what
  // the work item is and routing through chat only adds a guess in the middle.
  app.post("/api/work-items", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as {
      title?: string;
      brief?: string;
      repo?: string;
    };
    const title = (body.title ?? "").trim();
    if (!title) return c.json({ ok: false, error: "title required" }, 400);
    const item = await createWorkItem(title, "connector:web", {
      repo: body.repo?.trim() || undefined,
    });
    const brief = (body.brief ?? "").trim();
    if (brief) {
      await appendEvent({
        source: "connector:web",
        kind: "user.message",
        threadId: item.threadId,
        workItemId: item.id,
        payload: { text: brief },
      });
      // Dispatch detached: the manager may run for minutes.
      void handleThreadMessage(item.id, brief).catch(async (err) => {
        await appendEvent({
          source: "connector:web",
          kind: "agent.error",
          threadId: item.threadId,
          workItemId: item.id,
          payload: { error: String(err) },
        }).catch(() => {});
      });
    }
    return c.json({ ok: true, item, dispatched: brief.length > 0 }, 201);
  });

  // Worker output lives in the workspace and was otherwise unreachable: the
  // only way to read a produced file was to ask the agent to paste it back.
  app.get("/api/work-items/:id/files", async (c) => {
    try {
      const item = await getWorkItem(c.req.param("id"));
      return c.json({ workspace: item.workspace, files: await listArtifacts(item.workspace) });
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
  });

  app.get("/api/work-items/:id/file", async (c) => {
    const path = c.req.query("path") ?? "";
    if (!path) return c.json({ ok: false, error: "path required" }, 400);
    let item;
    try {
      item = await getWorkItem(c.req.param("id"));
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
    // The path comes from a URL; escaping the workspace must be impossible.
    if (!resolveInside(item.workspace, path)) {
      return c.json({ ok: false, error: "path outside workspace" }, 403);
    }
    if (c.req.query("download") !== undefined) {
      const target = resolveInside(item.workspace, path)!;
      const name = path.split("/").pop() ?? "file";
      return new Response(createReadStream(target) as unknown as ReadableStream, {
        headers: {
          "content-type": "application/octet-stream",
          "content-disposition": `attachment; filename="${encodeURIComponent(name)}"`,
        },
      });
    }
    const content = await readArtifact(item.workspace, path);
    if (!content) return c.json({ ok: false, error: "not found" }, 404);
    return c.json(content);
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

  // Stopping a runaway execution: without this the only option was waiting out
  // the 600s timeout while watching it go.
  app.post("/api/work-items/:id/cancel", async (c) => {
    const id = c.req.param("id");
    const executionId = activeExecutionId(id);
    if (!executionId && !hasActiveWorker(id)) {
      return c.json({ ok: false, error: "no running execution" }, 409);
    }
    // Intent before the effect, like every other side effect here — otherwise
    // the stop lands in the log after the execution it stopped.
    await appendEvent({
      source: "connector:web",
      kind: "execution.cancelled",
      workItemId: id,
      ...(executionId ? { executionId } : {}),
      payload: { reason: "cancelled from the web ui" },
    });
    const cancelled = await cancelActiveWorker(id);
    if (!cancelled) return c.json({ ok: false, error: "no running execution" }, 409);
    return c.json({ ok: true, executionId: executionId ?? null });
  });

  app.post("/api/chat", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as {
      text?: string;
      images?: { data?: unknown; mimeType?: unknown }[];
    };
    const text = (body.text ?? "").trim();
    const images = parseInboundImages(body.images);
    if (!text && images.length === 0) {
      return c.json({ ok: false, error: "text or images required" }, 400);
    }
    // An image-only message still needs words for the routing prompt; the same
    // stand-in the Feishu connector uses, so both channels read alike.
    const prompt = text || IMAGE_ONLY_TEXT;
    // Fire and forget: the fast lane records + routes; outcome arrives as events.
    void handleUserMessage(prompt, "connector:web", images).catch(async (err) => {
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

  // Distillation is the automatic path, but a user who already knows a
  // preference should not have to hint at it and hope the distiller notices.
  app.post("/api/memories", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as {
      kind?: string;
      content?: string;
    };
    const content = (body.content ?? "").trim();
    if (!content) return c.json({ ok: false, error: "content required" }, 400);
    const kind = (MEMORY_KINDS as string[]).includes(body.kind ?? "")
      ? (body.kind as MemoryKind)
      : "fact";
    const entry = await appendMemory(globalMemoryPath(), "global", { kind, content });
    await appendEvent({
      source: "connector:web",
      kind: "memory.promoted",
      payload: {
        memoryId: entry.id,
        kind: entry.kind,
        content: entry.content,
        scope: "global",
        // Provenance matters: this one was a person's decision, not a distillation.
        manual: true,
      },
    });
    return c.json({ ok: true, entry }, 201);
  });

  app.delete("/api/memories/:id", async (c) => {
    const ok = await forgetMemory(globalMemoryPath(), c.req.param("id"), "connector:web");
    return ok ? c.json({ ok: true }) : c.json({ ok: false, error: "not found" }, 404);
  });

  app.get("/api/schedules", async (c) => {
    return c.json({ schedules: await listSchedules() });
  });

  app.post("/api/schedules", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as ScheduleInput;
    try {
      return c.json({ ok: true, schedule: await createSchedule(body) }, 201);
    } catch (err) {
      // validateInput throws with a human-readable reason — surface it.
      return c.json({ ok: false, error: err instanceof Error ? err.message : String(err) }, 400);
    }
  });

  app.patch("/api/schedules/:id", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as Partial<ScheduleInput>;
    try {
      return c.json({ ok: true, schedule: await updateSchedule(c.req.param("id"), body) });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return c.json({ ok: false, error: message }, message.startsWith("schedule not found") ? 404 : 400);
    }
  });

  app.delete("/api/schedules/:id", async (c) => {
    try {
      await deleteSchedule(c.req.param("id"));
      return c.json({ ok: true });
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
  });

  // What a schedule has actually been doing. The firings are already facts in
  // the log; without a way to read them back, "last status: error" is a dead
  // end — you can see that it broke but not when it started or what it said.
  app.get("/api/schedules/:id/runs", async (c) => {
    const id = c.req.param("id");
    try {
      await getSchedule(id);
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
    const limit = Math.min(Number(c.req.query("limit") ?? 20), 100);
    // Query by the id inside the payload rather than scanning a tail window:
    // a daily schedule's previous run is thousands of heartbeats back, so a
    // window scan would report "no runs yet" for anything but the newest.
    // Each firing writes two events (intent, then outcome).
    const runs = await listEvents({
      payloadEquals: { key: "scheduleId", value: id },
      tail: limit * 2,
    });
    return c.json({ runs: runs.reverse() });
  });

  // Run-now: without it every definition mistake takes one full period to see.
  app.post("/api/schedules/:id/run", async (c) => {
    try {
      const schedule = await getSchedule(c.req.param("id"));
      const status = await fireSchedule(schedule);
      return c.json({ ok: true, status, schedule: await getSchedule(schedule.id) });
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
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
