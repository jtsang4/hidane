import type { Hono } from "hono";
import { streamSSE } from "hono/streaming";
import { appendEvent, getCursor, getEvent, listEvents, type HidaneEvent } from "../kernel/events.js";
import {
  createWorkItem,
  getWorkItem,
  listChildren,
  listWorkItems,
  setWorkItemDeadline,
} from "../kernel/workItems.js";
import { activeExecutionFor, busyWorkItemIds } from "../kernel/executions.js";
import { mailboxesWithPending } from "../kernel/mailbox.js";
import { onEventAppended } from "../kernel/notify.js";
import {
  addGlobalRule,
  globalPolicyPath,
  readPolicy,
  removeGlobalRule,
  validatePattern,
} from "../kernel/policies.js";
import { renderDay, today } from "../projections/worklog.js";
import { buildBoard } from "../projections/board.js";
import {
  conversationDays,
  recentConversation,
  searchConversation,
  searchWorkItems,
  titlesFor,
} from "../projections/conversation.js";
import { changeStatus, reattribute, redactMessage, submitMessage } from "../agents/ingress.js";
import { cancelTree, poolStatus } from "../agents/workerPool.js";
import { runtimeStatus } from "../agents/runtime.js";
import { liveTextSnapshot, subscribeLiveText, type LiveTextFrame } from "../agents/liveText.js";
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
 * Fallback cadence for the durable log. Appends wake the stream through the
 * database's NOTIFY; this only bounds how late a missed notification lands.
 */
const SSE_POLL_MS = 5000;

/**
 * Where a stream should resume from, as an exclusive seq.
 *
 * Every `hidane` frame is written with `id: seq`, so a browser reconnecting an
 * `EventSource` hands that seq back in `Last-Event-ID` without being asked.
 * Ignoring it made a reconnect start at the tail, dropping whatever was
 * appended while the connection was down: the client only recovered when some
 * *later* event happened to trigger a refetch, and if none came the view stayed
 * stale with no sign of it. An explicit `after` still wins — that is the caller
 * saying where to start, rather than the browser saying where it left off.
 *
 * A value that is not a whole, non-negative number is treated as absent rather
 * than coerced: `Number("")` is 0, which would replay the entire log.
 */
export function resumeCursor(
  after: string | undefined,
  lastEventId: string | undefined,
): number {
  for (const candidate of [after, lastEventId]) {
    if (candidate === undefined || candidate.trim() === "") continue;
    const seq = Number(candidate);
    if (Number.isInteger(seq) && seq >= 0) return seq;
    // A malformed `after` is the caller's error and must not silently fall
    // through to the browser's resume point, which would answer a different
    // question than the one asked.
    return Number.MAX_SAFE_INTEGER;
  }
  return Number.MAX_SAFE_INTEGER;
}

/**
 * Read API = queries over the event log, its state tables and projections.
 * Write API = the message door (`/api/chat`, optionally addressed to a work
 * item) and direct state operations; they return once the intent is recorded
 * and delivered, and answers arrive as events over SSE.
 */
export function registerApi(app: Hono): void {
  app.get("/api/events", async (c) => {
    const q = c.req.query();
    // `kind` accepts a comma-separated list. A single kind keeps the old
    // meaning, so existing callers and bookmarked filters are unaffected.
    const rawKind = q["kind"];
    const kinds = rawKind?.includes(",")
      ? rawKind.split(",").map((k) => k.trim()).filter(Boolean)
      : undefined;
    const filters = {
      conversation: q["conversation"] !== undefined,
      personOnly: q["conversation"] !== undefined && q["origin"] === "person",
      threadId: q["thread"],
      workItemId: q["item"],
      ...(kinds ? { kinds } : { kind: rawKind }),
      day: q["day"],
    };
    // Cursor pagination walks backwards through the log: `before` is exclusive,
    // matching the append-only spine's monotonic seq (same idea as consumer
    // cursors). One extra row tells us whether an older page exists.
    if (q["before"] !== undefined || q["page"] !== undefined) {
      const limit = Math.min(Number(q["limit"] ?? 50), 200);
      const titled = async (events: HidaneEvent[]) =>
        filters.conversation ? await titlesFor(events) : {};
      // A window around one event: how a search hit, a link or a day is
      // opened without paging through everything newer first. Walks both ways.
      if (q["around"] !== undefined) {
        const target = await getEvent(q["around"]);
        if (!target) return c.json({ ok: false, error: "not found" }, 404);
        const half = Math.floor(limit / 2);
        const [older, newer] = await Promise.all([
          listEvents({ ...filters, beforeSeq: target.seq, tail: half + 1 }),
          listEvents({ ...filters, afterSeq: target.seq - 1, limit: limit - half + 1 }),
        ]);
        const hasMore = older.length > half;
        const hasNewer = newer.length > limit - half;
        const events = [
          ...(hasMore ? older.slice(1) : older),
          ...(hasNewer ? newer.slice(0, limit - half) : newer),
        ];
        return c.json({
          events,
          hasMore,
          hasNewer,
          oldestSeq: events[0]?.seq ?? null,
          newestSeq: events.at(-1)?.seq ?? null,
          titles: await titled(events),
        });
      }
      if (q["after"] !== undefined) {
        const after = Number(q["after"]);
        if (!Number.isInteger(after) || after < 0) {
          return c.json({ ok: false, error: "after must be a seq" }, 400);
        }
        const page = await listEvents({ ...filters, afterSeq: after, limit: limit + 1 });
        const hasNewer = page.length > limit;
        const events = hasNewer ? page.slice(0, limit) : page;
        return c.json({
          events,
          hasMore: true,
          hasNewer,
          oldestSeq: events[0]?.seq ?? null,
          newestSeq: events.at(-1)?.seq ?? null,
          titles: await titled(events),
        });
      }
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
        hasNewer: false,
        oldestSeq: events[0]?.seq ?? null,
        newestSeq: events.at(-1)?.seq ?? null,
        titles: await titled(events),
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
    const after = resumeCursor(c.req.query("after"), c.req.header("last-event-id"));
    return streamSSE(c, async (stream) => {
      let open = true;
      /**
       * In-flight reply text, pushed rather than polled.
       *
       * The loop below is a database poller, which is the right shape for the
       * durable log but far too slow for a reply being typed out: a token would
       * wait up to a full poll cycle. These frames arrive on an in-process bus
       * and interrupt the wait, so they reach the reader as they are produced.
       *
       * Snapshot before subscribing, with no await between the two statements,
       * so a frame emitted in between can be neither lost nor counted twice.
       */
      const pendingFrames: LiveTextFrame[] = liveTextSnapshot();
      let wake: (() => void) | undefined;
      const unsubscribe = subscribeLiveText((frame) => {
        pendingFrames.push(frame);
        wake?.();
      });
      let appended = false;
      const unlisten = onEventAppended(() => {
        appended = true;
        wake?.();
      });
      stream.onAbort(() => {
        open = false;
        wake?.();
      });
      try {
        // Greet before touching the database. The tail lookup used to run first,
        // and when it failed under connection pressure the response was already
        // committed with 200 headers and no body at all — one connection in eight
        // under concurrent load. A client cannot distinguish that from a hang.
        await stream.writeSSE({ event: "hello", data: JSON.stringify({ after }) });
        let lastWrite = Date.now();
        let cursor = after;
        while (open) {
          // Drained first: a delta is worth less the later it lands, and the
          // database round-trip below is not free.
          while (pendingFrames.length > 0) {
            const frame = pendingFrames.shift();
            if (!frame) break;
            await stream.writeSSE({ event: "stream", data: JSON.stringify(frame) });
            lastWrite = Date.now();
          }
          appended = false;
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
          // Frames that arrived while the query was in flight woke nobody —
          // `wake` is only armed during the sleep. Skipping it keeps them from
          // waiting out a full cycle they were meant to interrupt.
          if (!open || pendingFrames.length > 0 || appended) {
            appended = false;
            continue;
          }
          await new Promise<void>((resolve) => {
            const timer = setTimeout(() => {
              wake = undefined;
              resolve();
            }, SSE_POLL_MS);
            wake = () => {
              clearTimeout(timer);
              wake = undefined;
              resolve();
            };
          });
        }
      } finally {
        unsubscribe();
        unlisten();
      }
    });
  });

  app.get("/api/work-items", async (c) => {
    const all = c.req.query("all") !== undefined;
    const items = await listWorkItems(all ? undefined : "open");
    // Which items are busy comes from the executions table: durable, and not
    // inferred from a window of recent events that a long run pushes out.
    const busy = new Set(await busyWorkItemIds());
    const running = items.filter((i) => busy.has(i.id)).map((i) => i.id);
    return c.json({ items, running });
  });

  app.get("/api/work-items/:id", async (c) => {
    try {
      const item = await getWorkItem(c.req.param("id"));
      // Bounded tail over everything about the item, whichever thread it was
      // written on; older events page in through /api/events?item=.
      const limit = Math.min(Number(c.req.query("limit") ?? 200), 500);
      const page = await listEvents({ workItemId: item.id, tail: limit + 1 });
      const hasMore = page.length > limit;
      const events = hasMore ? page.slice(page.length - limit) : page;
      const [execution, children] = await Promise.all([
        activeExecutionFor(item.id),
        listChildren(item.id),
      ]);
      return c.json({
        item,
        events,
        hasMore,
        running: execution !== undefined,
        execution: execution ?? null,
        children,
      });
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
      parentId?: string;
    };
    const title = (body.title ?? "").trim();
    if (!title) return c.json({ ok: false, error: "title required" }, 400);
    if (body.parentId) {
      try {
        await getWorkItem(body.parentId);
      } catch {
        return c.json({ ok: false, error: "parent not found" }, 400);
      }
    }
    const item = await createWorkItem(title, "connector:web", {
      repo: body.repo?.trim() || undefined,
      parentId: body.parentId,
    });
    const brief = (body.brief ?? "").trim();
    // The brief is something the person said to this item: it enters through
    // the same door as any message, addressed explicitly.
    if (brief) await submitMessage({ text: brief, source: "connector:web", target: item.id });
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
    const body = (await c.req.json().catch(() => ({}))) as {
      status?: string;
      deadlineAt?: string | null;
    };
    const id = c.req.param("id");
    const { status, deadlineAt } = body;
    if (status === undefined && deadlineAt === undefined) {
      return c.json({ ok: false, error: "status or deadlineAt required" }, 400);
    }
    if (status !== undefined && status !== "open" && status !== "done" && status !== "closed") {
      return c.json({ ok: false, error: "status must be open | done | closed" }, 400);
    }
    if (deadlineAt !== undefined && deadlineAt !== null && Number.isNaN(Date.parse(deadlineAt))) {
      return c.json({ ok: false, error: "deadlineAt must be an ISO timestamp or null" }, 400);
    }
    try {
      let item = await getWorkItem(id);
      if (status !== undefined) item = await changeStatus(id, status, "connector:web");
      if (deadlineAt !== undefined) item = await setWorkItemDeadline(id, deadlineAt, "connector:web");
      return c.json({ ok: true, item });
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
  });

  // Stops the item and everything under it. Without this the only option was
  // waiting out the 600s timeout while watching it go.
  app.post("/api/work-items/:id/cancel", async (c) => {
    const id = c.req.param("id");
    try {
      await getWorkItem(id);
    } catch {
      return c.json({ ok: false, error: "not found" }, 404);
    }
    const cancelled = await cancelTree(id, "cancelled from the web ui", "connector:web");
    if (cancelled.length === 0) return c.json({ ok: false, error: "no running execution" }, 409);
    return c.json({ ok: true, cancelled });
  });

  app.post("/api/chat", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as {
      text?: string;
      images?: { data?: unknown; mimeType?: unknown }[];
      target?: string;
      replyTo?: string;
      focus?: boolean;
    };
    const text = (body.text ?? "").trim();
    const images = parseInboundImages(body.images);
    if (!text && images.length === 0) {
      return c.json({ ok: false, error: "text or images required" }, 400);
    }
    if (body.target) {
      try {
        await getWorkItem(body.target);
      } catch {
        return c.json({ ok: false, error: "target not found" }, 404);
      }
    }
    // An image-only message still needs words for the routing prompt; the same
    // stand-in the Feishu connector uses, so both channels read alike.
    const message = await submitMessage({
      text: text || IMAGE_ONLY_TEXT,
      images,
      source: "connector:web",
      ...(body.target ? { target: body.target, focus: body.focus === true } : {}),
      ...(body.replyTo ? { replyTo: body.replyTo } : {}),
    });
    return c.json({ ok: true, accepted: true, messageId: message.id }, 202);
  });

  // The person corrects where a message went, or answers "which one?".
  app.post("/api/messages/:id/route", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as { workItemId?: string; title?: string };
    const messageId = c.req.param("id");
    let workItemId = body.workItemId;
    if (!workItemId) return c.json({ ok: false, error: "workItemId required" }, 400);
    try {
      if (workItemId === "new") {
        const message = await getEvent(messageId);
        if (!message || message.kind !== "user.message") throw new Error("message not found");
        const title = (body.title ?? String(message.payload["text"] ?? "")).trim().slice(0, 60) || "新任务";
        const item = await createWorkItem(title, "connector:web", { of: messageId });
        workItemId = item.id;
      }
      await reattribute(messageId, workItemId, "connector:web");
      return c.json({ ok: true, workItemId });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return c.json({ ok: false, error: message }, 404);
    }
  });

  // Everything ever said, not just what a reader has loaded.
  app.get("/api/conversation/search", async (c) => {
    const query = (c.req.query("q") ?? "").trim();
    if (!query) return c.json({ ok: false, error: "q required" }, 400);
    const rawBefore = c.req.query("before");
    const before = rawBefore !== undefined ? Number(rawBefore) : undefined;
    const limit = Number(c.req.query("limit") ?? 20);
    const [page, items] = await Promise.all([
      searchConversation({ query, before, limit: Number.isFinite(limit) ? limit : 20 }),
      // Items only with the first page: they are a short list, not a feed.
      before === undefined ? searchWorkItems(query) : Promise.resolve([]),
    ]);
    return c.json({ ...page, items, titles: await titlesFor(page.events) });
  });

  app.get("/api/conversation/days", async (c) => {
    return c.json({ days: await conversationDays(c.req.query("tz")) });
  });

  // Where the Primary's view of the conversation currently begins; the view
  // marks it so nobody assumes the assistant remembers everything above.
  app.get("/api/conversation/context", async (c) => {
    const recent = await recentConversation();
    return c.json({ fromId: recent.fromId, turns: recent.turns });
  });

  // Hide something the person said (a pasted secret, a wrong message). The
  // original row stays — the log is append-only — but every reader masks it.
  app.post("/api/messages/:id/redact", async (c) => {
    try {
      const event = await redactMessage(c.req.param("id"), "connector:web");
      return c.json({ ok: true, eventId: event?.id ?? null });
    } catch (err) {
      return c.json({ ok: false, error: err instanceof Error ? err.message : String(err) }, 404);
    }
  });

  app.get("/api/board", async (c) => {
    return c.json({ cards: await buildBoard(runtimeStatus()?.activeTurns ?? []) });
  });

  app.get("/api/policies", async (c) => {
    const policy = await readPolicy(globalPolicyPath());
    return c.json({ path: globalPolicyPath(), rules: policy.rules });
  });

  app.post("/api/policies", async (c) => {
    const body = (await c.req.json().catch(() => ({}))) as {
      pattern?: string;
      reason?: string;
      tools?: string[];
    };
    const pattern = (body.pattern ?? "").trim();
    const reason = (body.reason ?? "").trim();
    const invalid = validatePattern(pattern);
    if (invalid) return c.json({ ok: false, error: invalid }, 400);
    if (!reason) return c.json({ ok: false, error: "reason required" }, 400);
    const tools = Array.isArray(body.tools)
      ? body.tools.filter((t): t is string => typeof t === "string" && t.trim() !== "")
      : undefined;
    const rule = await addGlobalRule({ pattern, reason, tools });
    return c.json({ ok: true, rule }, 201);
  });

  app.delete("/api/policies/:id", async (c) => {
    const ok = await removeGlobalRule(c.req.param("id"));
    return ok ? c.json({ ok: true }) : c.json({ ok: false, error: "not found" }, 404);
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
    const mailboxes = await mailboxesWithPending();
    return c.json({
      latestSeq,
      triageCursor: cursor,
      triageLag: Math.max(0, latestSeq - cursor),
      lastHeartbeatAt: heartbeats[0]?.ts ?? null,
      openWorkItems: open.length,
      model,
      runtime: {
        up: runtimeStatus() !== undefined,
        activeTurns: runtimeStatus()?.activeTurns ?? [],
        pendingMailboxes: mailboxes.length,
        pendingMessages: mailboxes.reduce((n, m) => n + m.count, 0),
        workers: poolStatus(),
      },
    });
  });
}
