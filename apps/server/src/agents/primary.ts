import { appendEvent, getEvent, listEvents, type HidaneEvent } from "../kernel/events.js";
import { post, rootOf } from "../kernel/mailbox.js";
import { busyWorkItemIds } from "../kernel/executions.js";
import {
  createWorkItem,
  getWorkItem,
  listWorkItems,
  type WorkItem,
} from "../kernel/workItems.js";
import { config } from "../config.js";
import { PRIMARY_CHARTER } from "./charters.js";
import { openPrimarySession } from "./sdk.js";
import { PRIMARY } from "./addresses.js";
import { describeRecall, recentConversation, searchConversation } from "../projections/conversation.js";
import { recallForPrimary } from "./distiller.js";
import { loadImages, storedImagesOf } from "./inbox.js";
import { nowLine, str, think, type Effect } from "./think.js";
import { changeStatus, deliverToWorkItem } from "./ingress.js";
import { cancelTree } from "./workerPool.js";

type WorkItemStatus = WorkItem["status"];

/** Message kinds the Primary's model reads; everything else is handled by rule. */
const ROUTABLE = new Set([
  "user.message",
  "triage.decision",
  "schedule.prompt",
  "message.reroute_requested",
  "conversation.recalled",
]);

function isWorkItemStatus(value: unknown): value is WorkItemStatus {
  return value === "open" || value === "done" || value === "closed";
}

function parseIds(raw: unknown): { present: boolean; ids: string[]; malformed: boolean } {
  if (raw === undefined) return { present: false, ids: [], malformed: false };
  if (!Array.isArray(raw)) return { present: true, ids: [], malformed: true };
  const malformed = raw.some((id) => typeof id !== "string" || !id.trim());
  const ids = Array.from(
    new Set(
      raw
        .filter((id): id is string => typeof id === "string")
        .map((id) => id.trim())
        .filter(Boolean),
    ),
  );
  return { present: true, ids, malformed };
}

/** One line per message, tagged with its id so effects can say what they answer. */
function describe(m: HidaneEvent): string {
  const text = String(m.payload["text"] ?? "");
  switch (m.kind) {
    case "triage.decision":
      return `[${m.id}] (external: ${String(m.payload["ofKind"] ?? "event")}) ${String(m.payload["summary"] ?? "")}`;
    case "schedule.prompt":
      return `[${m.id}] (scheduled "${String(m.payload["name"] ?? "")}") ${String(m.payload["prompt"] ?? "")}`;
    case "conversation.recalled":
      return `[${m.id}] (recall: you searched earlier conversation for "${String(m.payload["query"] ?? "")}" to answer ${String(m.payload["of"] ?? "")}; answer that message now with of="${m.id}". Do not recall again.)\nThe message: ${String(m.payload["original"] ?? "")}\nFound:\n${String(m.payload["text"] ?? "")}`;
    case "message.reroute_requested":
      return `[${m.id}] (reroute: work item ${String((m.payload["exclude"] as string[] | undefined)?.join(", ") ?? "")} says this is not theirs — do not route it back there) ${text}`;
    default:
      return `[${m.id}] (user${m.payload["imageCount"] ? `, ${String(m.payload["imageCount"])} image(s) attached` : ""}) ${text}`;
  }
}

/** Where an answer to this message belongs in the conversation, and how to title it. */
function rootMeta(m: HidaneEvent): Record<string, unknown> {
  const meta: Record<string, unknown> = { of: m.id, root: rootOf(m) };
  // A recall answers the message it was made for, and takes its framing along.
  if (m.kind === "conversation.recalled") {
    for (const key of ["rootKind", "rootText"]) {
      if (m.payload[key] !== undefined) meta[key] = m.payload[key];
    }
    return meta;
  }
  if (m.kind === "triage.decision") {
    meta["rootKind"] = "external";
    meta["rootText"] = String(m.payload["summary"] ?? "").slice(0, 200);
  } else if (m.kind === "schedule.prompt") {
    meta["rootKind"] = "scheduled";
    meta["rootText"] = String(m.payload["name"] ?? "");
  }
  return meta;
}

/**
 * A bubbled question that reached the top: the Primary has no tools and no
 * better knowledge than the Manager that asked, so it is handed to the person
 * as-is, with the path it took to get here.
 */
async function surfaceEscalation(m: HidaneEvent): Promise<void> {
  await appendEvent({
    source: "agent:primary",
    kind: "escalation",
    threadId: "main",
    workItemId: m.workItemId ?? undefined,
    causedBy: m.id,
    hop: m.hop + 1,
    payload: {
      question: m.payload["question"],
      path: m.payload["path"] ?? [],
      of: m.id,
      root: rootOf(m),
      reason: "question",
    },
  });
}

class TurnContext {
  readonly covered = new Set<string>();
  constructor(
    readonly batch: HidaneEvent[],
    readonly all: WorkItem[],
    readonly busy: string[],
  ) {}

  message(of: unknown): HidaneEvent | undefined {
    return typeof of === "string" ? this.batch.find((m) => m.id === of) : undefined;
  }

  cover(m: HidaneEvent, also?: unknown): void {
    this.covered.add(m.id);
    if (Array.isArray(also)) {
      for (const id of also) if (typeof id === "string") this.covered.add(id);
    }
  }

  get open(): WorkItem[] {
    return this.all.filter((i) => i.status === "open");
  }

  async reply(m: HidaneEvent, text: string): Promise<void> {
    await appendEvent({
      source: "agent:primary",
      kind: "agent.reply",
      threadId: "main",
      causedBy: m.id,
      hop: m.hop + 1,
      payload: { text, ...rootMeta(m) },
    });
  }
}

/** The message whose attribution an effect decides — a reroute decides for the original. */
async function attributionSubject(m: HidaneEvent): Promise<HidaneEvent> {
  if (m.kind !== "message.reroute_requested") return m;
  const original = await getEvent(String(m.payload["of"] ?? ""));
  return original ?? m;
}

async function applyEffect(ctx: TurnContext, e: Effect): Promise<void> {
  const m = ctx.message(e["of"]);
  if (!m) return;

  // Looking further back than the recent conversation. The turn does not wait
  // for it: the findings come back as the next message in this mailbox.
  if (e.type === "recall") {
    const query = str(e["query"]);
    // One look back per message; a recall result must be answered as is.
    if (!query || m.kind === "conversation.recalled") return;
    ctx.cover(m, e["also_of"]);
    const found = await searchConversation({ query, limit: 8 });
    const hits = found.events.filter((hit) => !ctx.batch.some((b) => b.id === hit.id));
    const meta = rootMeta(m);
    await post({
      source: "agent:primary",
      kind: "conversation.recalled",
      mailbox: PRIMARY,
      lane: "interrupt",
      threadId: "main",
      causedBy: m,
      payload: {
        of: m.id,
        root: meta["root"],
        ...(meta["rootKind"] !== undefined ? { rootKind: meta["rootKind"], rootText: meta["rootText"] } : {}),
        query,
        original: describe(m),
        hits: hits.map((hit) => hit.id),
        text: describeRecall(hits),
      },
    });
    return;
  }

  if (e.type === "reply") {
    ctx.cover(m, e["also_of"]);
    await ctx.reply(m, str(e["reply"]) ?? "");
    return;
  }

  if (e.type === "ambiguous" || e.type === "route") {
    const excluded = (m.payload["exclude"] as string[] | undefined) ?? [];
    const target = str(e["work_item_id"]);
    const confidence = typeof e["confidence"] === "number" ? e["confidence"] : undefined;
    // Never back to the Manager that refused it.
    if (e.type === "route" && target && excluded.includes(target)) return;
    const item = ctx.open.find((i) => i.id === target);
    if (e.type === "route" && item && (confidence ?? 1) >= config.attributionThreshold) {
      ctx.cover(m, e["also_of"]);
      await deliverToWorkItem(await attributionSubject(m), item, "model", {
        text: str(e["message"]),
        ...(confidence !== undefined ? { confidence } : {}),
        source: "agent:primary",
      });
      return;
    }
    // Asked, or not sure enough: put the choice in front of the person rather
    // than guessing — a wrong guess sends work into the wrong workspace.
    const raw = e.type === "route" ? [target, "new"] : (e["candidates"] as unknown[] | undefined) ?? [];
    const candidates = raw
      .map((c) => (c === "new" ? { workItemId: "new", title: "" } : ctx.open.find((i) => i.id === c)))
      .filter((c): c is { workItemId: string; title: string } | WorkItem => c !== undefined)
      .filter((c) => !("id" in c) || !excluded.includes(c.id))
      .map((c) => ("id" in c ? { workItemId: c.id, title: c.title } : c));
    if (candidates.length === 0) return;
    ctx.cover(m, e["also_of"]);
    const subject = await attributionSubject(m);
    await appendEvent({
      source: "agent:primary",
      kind: "attribution.ambiguous",
      threadId: "main",
      causedBy: m.id,
      hop: m.hop + 1,
      payload: {
        of: subject.id,
        root: rootOf(subject),
        question:
          str(e["reply"]) ??
          (item ? `这条消息是关于「${item.title}」的吗？` : "这条消息属于哪个任务？"),
        candidates,
      },
    });
    return;
  }

  if (e.type === "create_work_item") {
    if (e["dispatch"] !== undefined && typeof e["dispatch"] !== "boolean") {
      ctx.cover(m, e["also_of"]);
      await ctx.reply(m, "无法创建工作项：dispatch 必须是布尔值。");
      return;
    }
    ctx.cover(m, e["also_of"]);
    const subject = await attributionSubject(m);
    const title = str(e["title"]) ?? String(subject.payload["text"] ?? "").slice(0, 60);
    const item = await createWorkItem(title, "agent:primary", {
      repo: str(e["repo"]),
      of: subject.id,
    });
    const brief = str(e["brief"]) ?? String(subject.payload["text"] ?? "");
    if (e["dispatch"] === false) {
      await appendEvent({
        source: "agent:primary",
        kind: "message.attributed",
        threadId: "main",
        workItemId: item.id,
        causedBy: m.id,
        payload: { of: subject.id, workItemId: item.id, title: item.title, by: "model", created: true },
      });
      await appendEvent({
        source: "agent:primary",
        kind: "user.message",
        threadId: item.threadId,
        workItemId: item.id,
        payload: { text: brief, of: subject.id, root: rootOf(subject), forwardedFrom: "main", deferred: true },
      });
      await ctx.reply(m, `已创建工作项 ${item.id}，状态为 open，暂未启动 Manager/Worker。`);
      return;
    }
    await deliverToWorkItem(subject, item, "model", {
      text: brief,
      created: true,
      source: "agent:primary",
    });
    // Later messages of the same batch folded into this one reach the Manager
    // too, so none of the person's refinements is dropped.
    for (const id of Array.isArray(e["also_of"]) ? e["also_of"] : []) {
      const extra = ctx.message(id);
      if (!extra || extra.id === m.id) continue;
      await deliverToWorkItem(extra, item, "model", { source: "agent:primary" });
    }
    return;
  }

  if (e.type === "set_status") {
    ctx.cover(m);
    const parsed = parseIds(e["work_item_ids"]);
    const allOpen = e["all_open"] === true;
    const allItems = e["all_items"] === true;
    const status = e["status"];
    const selectors = Number(parsed.present) + Number(allOpen) + Number(allItems);
    if (
      !isWorkItemStatus(status) ||
      parsed.malformed ||
      (e["all_open"] !== undefined && typeof e["all_open"] !== "boolean") ||
      (e["all_items"] !== undefined && typeof e["all_items"] !== "boolean") ||
      selectors !== 1
    ) {
      await ctx.reply(
        m,
        "无法执行工作项状态变更：需要从工作项清单中选择 ID，或使用 all_open/all_items，并指定 open、done 或 closed。",
      );
      return;
    }
    const targetIds = allOpen
      ? ctx.open.map((i) => i.id)
      : allItems
        ? ctx.all.map((i) => i.id)
        : parsed.ids;
    const unknown = targetIds.filter((id) => !ctx.all.some((i) => i.id === id));
    if (unknown.length > 0) {
      await ctx.reply(m, `无法变更这些工作项：${unknown.join("、")}。只能操作当前工作项清单中的 ID。`);
      return;
    }
    // Re-read before changing: another channel may have changed an item while
    // the model was thinking, and a bulk result must not misreport it.
    const current = await Promise.all(targetIds.map((id) => getWorkItem(id)));
    const noLongerOpen = allOpen ? current.filter((i) => i.status !== "open").map((i) => i.id) : [];
    if (noLongerOpen.length > 0) {
      await ctx.reply(m, `无法变更这些工作项：${noLongerOpen.join("、")} 已不再是开放状态。`);
      return;
    }
    const changed: string[] = [];
    for (const item of current) {
      if (item.status === status) continue;
      await changeStatus(item.id, status, "agent:primary", m);
      changed.push(item.id);
    }
    await ctx.reply(
      m,
      changed.length > 0
        ? `已将 ${changed.length} 个工作项的状态设为 ${status}：${changed.join("、")}`
        : `没有工作项需要变更（目标状态：${status}）。`,
    );
    return;
  }

  if (e.type === "cancel") {
    ctx.cover(m);
    const parsed = parseIds(e["work_item_ids"]);
    const allRunning = e["all_running"] === true;
    if (
      parsed.malformed ||
      (e["all_running"] !== undefined && typeof e["all_running"] !== "boolean") ||
      Number(parsed.present) + Number(allRunning) !== 1
    ) {
      await ctx.reply(m, "无法中止执行：需要从正在运行的执行清单中选择 ID，或使用 all_running:true。");
      return;
    }
    const targetIds = allRunning ? ctx.busy : parsed.ids;
    const unknown = targetIds.filter((id) => !ctx.all.some((i) => i.id === id));
    if (unknown.length > 0) {
      await ctx.reply(m, `无法中止这些工作项：${unknown.join("、")} 不在工作项清单中。`);
      return;
    }
    const cancelled: string[] = [];
    for (const id of targetIds) {
      cancelled.push(...(await cancelTree(id, "cancelled from the primary agent", "agent:primary")));
    }
    await ctx.reply(
      m,
      cancelled.length > 0
        ? `已请求中止 ${cancelled.length} 个正在运行的执行：${cancelled.join("、")}`
        : "没有可中止的正在运行的执行。",
    );
  }
}

/**
 * The Primary's turn: every message that reached it since its last turn,
 * decided together. Rules first — a bubbled question goes straight to the
 * person — and only what needs judgment reaches the model.
 */
export async function primaryTurn(_address: string, messages: HidaneEvent[]): Promise<void> {
  const batch: HidaneEvent[] = [];
  for (const m of messages) {
    if (m.kind === "escalation.raised") await surfaceEscalation(m);
    else if (ROUTABLE.has(m.kind)) batch.push(m);
  }
  if (batch.length === 0) return;

  const [all, busy, memories, recent] = await Promise.all([
    listWorkItems(),
    busyWorkItemIds(),
    recallForPrimary(),
    recentConversation({ exclude: new Set(batch.map(rootOf)) }),
  ]);
  const ctx = new TurnContext(batch, all, busy);
  const understanding = await latestUnderstanding(ctx.open.map((i) => i.id));
  const line = (i: WorkItem) =>
    `- ${i.id} [${i.status}]${busy.includes(i.id) ? " [running]" : ""}${i.parentId ? ` (child of ${i.parentId})` : ""}: ${i.title}${understanding.get(i.id) ? ` — ${understanding.get(i.id)}` : ""}`;
  const openList = ctx.open.length > 0 ? ctx.open.map(line).join("\n") : "(none)";
  const allList = all.length > 0 ? all.map(line).join("\n") : "(none)";
  const runningList =
    busy.length > 0
      ? all.filter((i) => busy.includes(i.id)).map((i) => `- ${i.id}: ${i.title}`).join("\n")
      : "(none)";

  const images = await loadImages(batch.flatMap((m) => storedImagesOf(m.payload)));
  const session = await openPrimarySession(PRIMARY_CHARTER);
  const thought = await think(
    session,
    [
      nowLine(),
      memories,
      `Open work items (routing targets):\n${openList}`,
      `All work items (status management):\n${allList}`,
      `Running executions (cancellation targets):\n${runningList}`,
      recent.text,
      `Messages this turn:\n${batch.map(describe).join("\n")}`,
    ]
      .filter(Boolean)
      .join("\n\n"),
    { images, liveThreadId: "main" },
  ).finally(() => session.dispose());

  await appendEvent({
    source: "agent:primary",
    kind: "route.decision",
    threadId: "main",
    payload: {
      ok: thought.ok,
      durationMs: thought.durationMs,
      of: batch.map((m) => m.id),
      effects: (thought.effects ?? [{ type: "reply", raw: thought.raw.slice(0, 500) }]) as unknown[],
    },
  });

  if (!thought.effects) {
    const text = thought.ok
      ? thought.raw
      : `primary routing failed: ${thought.error ?? "unknown"}`;
    for (const m of batch) await ctx.reply(m, text);
    return;
  }
  for (const effect of thought.effects) {
    await applyEffect(ctx, effect);
  }
  // A message the model skipped would otherwise sit "routing…" forever.
  for (const m of batch) {
    if (ctx.covered.has(m.id)) continue;
    if (m.kind === "triage.decision") continue;
    await ctx.reply(m, "这条消息没有被处理，请换个说法再试一次。");
  }
}

async function latestUnderstanding(ids: string[]): Promise<Map<string, string>> {
  const map = new Map<string, string>();
  if (ids.length === 0) return map;
  const events = await listEvents({ kind: "work_item.understanding", tail: 200 });
  for (const e of events) {
    if (e.workItemId && ids.includes(e.workItemId)) {
      map.set(e.workItemId, String(e.payload["text"] ?? "").slice(0, 200));
    }
  }
  return map;
}
