import { appendEvent } from "../kernel/events.js";
import {
  createWorkItem,
  getWorkItem,
  listWorkItems,
  setWorkItemStatus,
} from "../kernel/workItems.js";
import { config } from "../config.js";
import { extractJson } from "./pi.js";
import { beginLiveText } from "./liveText.js";
import { createReplyExtractor } from "./replyStream.js";
import { PRIMARY_CHARTER } from "./charters.js";
import { getPrimarySession, promptRole } from "./sdk.js";
import { handleThreadMessage } from "./manager.js";
import { recallForPrimary } from "./distiller.js";
import { activeExecutionId, cancelActiveWorker, hasActiveWorker } from "./rpcWorker.js";

type WorkItemStatus = "open" | "done" | "closed";

interface RouteDecision {
  action:
    | "reply"
    | "new_work_item"
    | "route_to_work_item"
    | "set_work_item_status"
    | "cancel_work_item_execution";
  reply?: string;
  title?: string;
  brief?: string;
  repo?: string | null;
  work_item_id?: string;
  work_item_ids?: unknown;
  message?: string;
  dispatch?: unknown;
  all_open?: unknown;
  all_items?: unknown;
  all_running?: unknown;
  status?: unknown;
}

export interface PrimaryOutcome {
  action: RouteDecision["action"] | "fallback_reply";
  reply: string;
  workItemId?: string | undefined;
  workItemIds?: string[] | undefined;
}

/**
 * A routed message is answered in the work-item thread by the Manager, but
 * the user sent it from the main thread. Mirror the completed answer back to
 * that thread so clients waiting on the main conversation can observe the
 * terminal reply instead of waiting forever for an answer on another thread.
 */
async function appendMainThreadReply(workItemId: string, text: string): Promise<void> {
  await appendEvent({
    source: "agent:manager",
    kind: "agent.reply",
    threadId: "main",
    workItemId,
    payload: { text },
  });
}

async function appendPrimaryReply(text: string): Promise<void> {
  await appendEvent({
    source: "agent:primary",
    kind: "agent.reply",
    threadId: "main",
    payload: { text },
  });
}

function isWorkItemStatus(value: unknown): value is WorkItemStatus {
  return value === "open" || value === "done" || value === "closed";
}

function parseWorkItemIds(raw: unknown): {
  present: boolean;
  ids: string[];
  malformed: boolean;
} {
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

/**
 * Fast lane: a user message reaches the Primary directly (no triage queue),
 * is recorded to the log, routed, and answered synchronously.
 * The Primary is a persistent SDK session — one identity across turns.
 */
export async function handleUserMessage(
  text: string,
  source = "connector:cli",
  images: { data: string; mimeType: string }[] = [],
): Promise<PrimaryOutcome> {
  await appendEvent({
    source,
    kind: "user.message",
    threadId: "main",
    payload: { text, ...(images.length > 0 ? { imageCount: images.length } : {}) },
  });

  const all = await listWorkItems();
  const open = all.filter((item) => item.status === "open");
  const running = all.filter((item) => hasActiveWorker(item.id));
  const itemsList =
    all.length > 0
      ? all
          .map(
            (item) =>
              `- ${item.id} [${item.status}]${running.some((run) => run.id === item.id) ? " [running]" : ""}: ${item.title}`,
          )
          .join("\n")
      : "(none)";
  const openItemsList =
    open.length > 0 ? open.map((item) => `- ${item.id}: ${item.title}`).join("\n") : "(none)";
  const runningItemsList =
    running.length > 0
      ? running.map((item) => `- ${item.id}: ${item.title}`).join("\n")
      : "(none)";

  const memories = await recallForPrimary();
  const session = await getPrimarySession(PRIMARY_CHARTER);
  /**
   * Show the answer as it is written instead of after it is finished.
   *
   * Closed the moment the model stops, not when the reply is appended: the two
   * are a database round-trip and one poll cycle apart, and holding the
   * provisional bubble "live" across that gap would leave a typing cursor
   * blinking under text that is already complete. The client keeps rendering
   * the finished text until the durable event arrives and takes over.
   */
  const live = beginLiveText("main");
  const extract = createReplyExtractor();
  const routing = await promptRole(
    session,
    [
      memories,
      `Open work items (routing targets):\n${openItemsList}`,
      `All work items (status management):\n${itemsList}`,
      `Running executions (cancellation targets):\n${runningItemsList}`,
      `Incoming message:\n${text}`,
    ]
      .filter(Boolean)
      .join("\n\n"),
    config.routeTimeoutSec,
    images,
    (delta) => live.push(extract(delta)),
  ).finally(() => live.end());

  const decision = routing.ok ? extractJson<RouteDecision>(routing.text) : null;

  await appendEvent({
    source: "agent:primary",
    kind: "route.decision",
    threadId: "main",
    payload: {
      ok: routing.ok,
      durationMs: routing.durationMs,
      decision: (decision ?? {
        action: "reply",
        raw: routing.text.slice(0, 500),
      }) as Record<string, unknown>,
    } as Record<string, unknown>,
  });

  if (!decision) {
    const reply = routing.ok
      ? routing.text
      : `primary routing failed: ${routing.error ?? "unknown"}`;
    await appendPrimaryReply(reply);
    return { action: "fallback_reply", reply };
  }

  if (decision.action === "reply") {
    const reply = decision.reply ?? "";
    await appendPrimaryReply(reply);
    return { action: "reply", reply };
  }

  if (decision.action === "new_work_item") {
    if (decision.dispatch !== undefined && typeof decision.dispatch !== "boolean") {
      const reply = "无法创建工作项：dispatch 必须是布尔值。";
      await appendPrimaryReply(reply);
      return { action: "fallback_reply", reply };
    }
    const dispatch = decision.dispatch !== false;
    const title = decision.title ?? text.slice(0, 60);
    const item = await createWorkItem(title, "agent:primary", {
      repo: decision.repo ?? undefined,
    });
    const brief = typeof decision.brief === "string" && decision.brief.trim() ? decision.brief : text;
    await appendEvent({
      source: "agent:primary",
      kind: "user.message",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: brief, forwardedFrom: "main", ...(dispatch ? {} : { deferred: true }) },
    });
    if (!dispatch) {
      const reply = `已创建工作项 ${item.id}，状态为 open，暂未启动 Manager/Worker。`;
      await appendPrimaryReply(reply);
      return { action: "new_work_item", reply, workItemId: item.id };
    }
    const reply = await handleThreadMessage(item.id, brief);
    await appendMainThreadReply(item.id, reply);
    await appendEvent({
      source: "agent:manager",
      kind: "escalation",
      threadId: "main",
      workItemId: item.id,
      payload: { note: `work item ${item.id} (${title}) finished a run` },
    });
    return { action: "new_work_item", reply, workItemId: item.id };
  }

  if (decision.action === "set_work_item_status") {
    const parsedIds = parseWorkItemIds(decision.work_item_ids);
    const allOpen = decision.all_open === true;
    const allItems = decision.all_items === true;
    const status = decision.status;
    const selectorCount =
      Number(parsedIds.present) + Number(allOpen) + Number(allItems);
    const invalidSelection =
      parsedIds.malformed ||
      (decision.all_open !== undefined && typeof decision.all_open !== "boolean") ||
      (decision.all_items !== undefined && typeof decision.all_items !== "boolean") ||
      selectorCount !== 1;

    if (!isWorkItemStatus(status) || invalidSelection) {
      const reply =
        "无法执行工作项状态变更：需要从工作项清单中选择 ID，或使用 all_open/all_items，并指定 open、done 或 closed。";
      await appendPrimaryReply(reply);
      return { action: "fallback_reply", reply };
    }

    const targetIds = allOpen
      ? open.map((item) => item.id)
      : allItems
        ? all.map((item) => item.id)
        : parsedIds.ids;
    const unknownIds = targetIds.filter((id) => !all.some((item) => item.id === id));
    if (unknownIds.length > 0) {
      const reply = `无法变更这些工作项：${unknownIds.join("、")}。只能操作当前工作项清单中的 ID。`;
      await appendPrimaryReply(reply);
      return { action: "fallback_reply", reply };
    }

    // Re-read the targets before changing anything. The inventory snapshot can
    // be stale if another channel changed an item while the Primary was
    // thinking; an all-open request is rejected rather than silently acting on
    // a non-open item or reporting a misleading bulk result.
    const current = await Promise.all(targetIds.map((id) => getWorkItem(id)));
    const noLongerOpen = allOpen
      ? current.filter((item) => item.status !== "open").map((item) => item.id)
      : [];
    if (noLongerOpen.length > 0) {
      const reply = `无法变更这些工作项：${noLongerOpen.join("、")} 已不再是开放状态。`;
      await appendPrimaryReply(reply);
      return { action: "fallback_reply", reply };
    }

    const changed: string[] = [];
    for (const item of current) {
      if (item.status === status) continue;
      await setWorkItemStatus(item.id, status, "agent:primary");
      changed.push(item.id);
    }
    const reply =
      changed.length > 0
        ? `已将 ${changed.length} 个工作项的状态设为 ${status}：${changed.join("、")}`
        : `没有工作项需要变更（目标状态：${status}）。`;
    await appendPrimaryReply(reply);
    return { action: "set_work_item_status", reply, workItemIds: changed };
  }

  if (decision.action === "cancel_work_item_execution") {
    const parsedIds = parseWorkItemIds(decision.work_item_ids);
    const allRunning = decision.all_running === true;
    const selectorCount = Number(parsedIds.present) + Number(allRunning);
    const invalidSelection =
      parsedIds.malformed ||
      (decision.all_running !== undefined && typeof decision.all_running !== "boolean") ||
      selectorCount !== 1;

    if (invalidSelection) {
      const reply =
        "无法中止执行：需要从正在运行的执行清单中选择 ID，或使用 all_running:true。";
      await appendPrimaryReply(reply);
      return { action: "fallback_reply", reply };
    }

    const targetIds = allRunning ? running.map((item) => item.id) : parsedIds.ids;
    const unknownIds = targetIds.filter((id) => !running.some((item) => item.id === id));
    if (unknownIds.length > 0) {
      const reply = `无法中止这些工作项：${unknownIds.join("、")} 当前没有正在运行的执行。`;
      await appendPrimaryReply(reply);
      return { action: "fallback_reply", reply };
    }

    const cancelled: string[] = [];
    for (const id of targetIds) {
      const item = all.find((candidate) => candidate.id === id);
      if (!item || !hasActiveWorker(id)) continue;
      const executionId = activeExecutionId(id);
      await appendEvent({
        source: "agent:primary",
        kind: "execution.cancelled",
        threadId: item.threadId,
        workItemId: id,
        ...(executionId ? { executionId } : {}),
        payload: { reason: "cancelled from the primary agent" },
      });
      if (await cancelActiveWorker(id)) cancelled.push(id);
    }

    const reply =
      cancelled.length > 0
        ? `已请求中止 ${cancelled.length} 个正在运行的执行：${cancelled.join("、")}`
        : "没有可中止的正在运行的执行。";
    await appendPrimaryReply(reply);
    return { action: "cancel_work_item_execution", reply, workItemIds: cancelled };
  }

  // route_to_work_item
  const targetId = decision.work_item_id ?? "";
  const forwarded = decision.message ?? text;
  const known = open.find((i) => i.id === targetId);
  if (!known) {
    const reply = `routing pointed at unknown work item ${targetId}`;
    await appendEvent({
      source: "agent:primary",
      kind: "agent.reply",
      threadId: "main",
      payload: { text: reply },
    });
    return { action: "fallback_reply", reply };
  }
  await appendEvent({
    source: "agent:primary",
    kind: "user.message",
    threadId: known.threadId,
    workItemId: known.id,
    payload: { text: forwarded, forwardedFrom: "main" },
  });
  const reply = await handleThreadMessage(known.id, forwarded);
  await appendMainThreadReply(known.id, reply);
  return { action: "route_to_work_item", reply, workItemId: known.id };
}
