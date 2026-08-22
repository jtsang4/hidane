import { appendEvent } from "../kernel/events.js";
import { createWorkItem, listWorkItems } from "../kernel/workItems.js";
import { config } from "../config.js";
import { extractJson } from "./pi.js";
import { PRIMARY_CHARTER } from "./charters.js";
import { getPrimarySession, promptRole } from "./sdk.js";
import { handleThreadMessage } from "./manager.js";
import { recallForPrimary } from "./distiller.js";

interface RouteDecision {
  action: "reply" | "new_work_item" | "route_to_work_item";
  reply?: string;
  title?: string;
  brief?: string;
  repo?: string | null;
  work_item_id?: string;
  message?: string;
}

export interface PrimaryOutcome {
  action: RouteDecision["action"] | "fallback_reply";
  reply: string;
  workItemId?: string | undefined;
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

  const open = await listWorkItems("open");
  const itemsList =
    open.length > 0
      ? open.map((i) => `- ${i.id}: ${i.title}`).join("\n")
      : "(none)";

  const memories = await recallForPrimary();
  const session = await getPrimarySession(PRIMARY_CHARTER);
  const routing = await promptRole(
    session,
    [memories, `Open work items:\n${itemsList}`, `Incoming message:\n${text}`]
      .filter(Boolean)
      .join("\n\n"),
    config.routeTimeoutSec,
    images,
  );

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
    await appendEvent({
      source: "agent:primary",
      kind: "agent.reply",
      threadId: "main",
      payload: { text: reply },
    });
    return { action: "fallback_reply", reply };
  }

  if (decision.action === "reply") {
    const reply = decision.reply ?? "";
    await appendEvent({
      source: "agent:primary",
      kind: "agent.reply",
      threadId: "main",
      payload: { text: reply },
    });
    return { action: "reply", reply };
  }

  if (decision.action === "new_work_item") {
    const title = decision.title ?? text.slice(0, 60);
    const item = await createWorkItem(title, "agent:primary", {
      repo: decision.repo ?? undefined,
    });
    const brief = decision.brief ?? text;
    await appendEvent({
      source: "agent:primary",
      kind: "user.message",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: brief, forwardedFrom: "main" },
    });
    const reply = await handleThreadMessage(item.id, brief);
    await appendEvent({
      source: "agent:manager",
      kind: "escalation",
      threadId: "main",
      workItemId: item.id,
      payload: { note: `work item ${item.id} (${title}) finished a run` },
    });
    return { action: "new_work_item", reply, workItemId: item.id };
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
  return { action: "route_to_work_item", reply, workItemId: known.id };
}
