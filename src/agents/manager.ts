import { appendEvent, listEvents } from "../kernel/events.js";
import { getWorkItem } from "../kernel/workItems.js";
import { genId } from "../kernel/ids.js";
import { config } from "../config.js";
import { runPi, extractJson } from "./pi.js";
import { MANAGER_CHARTER, WORKER_CHARTER } from "./charters.js";
import { join } from "node:path";

interface ManagerDecision {
  instructions?: string | null;
  expect?: string;
  reply?: string;
}

/**
 * Manager: default addressee of a work item's thread. Plans one execution
 * per incoming message and spawns a worker in the work item's workspace.
 */
export async function handleThreadMessage(
  workItemId: string,
  message: string,
): Promise<string> {
  const item = await getWorkItem(workItemId);
  const history = await listEvents({ threadId: item.threadId, tail: 12 });
  const historyText = history
    .filter((e) => ["user.message", "agent.reply"].includes(e.kind))
    .map((e) => `[${e.kind}] ${String(e.payload["text"] ?? "")}`)
    .join("\n");

  const planning = await runPi({
    prompt: [
      `Work item: ${item.id} — ${item.title} (status: ${item.status})`,
      `Workspace: ${item.workspace}`,
      historyText ? `Recent thread:\n${historyText}` : "",
      `New message:\n${message}`,
    ]
      .filter(Boolean)
      .join("\n\n"),
    charter: MANAGER_CHARTER,
    tools: false,
    skills: false,
    thinking: config.routeThinking,
    timeoutSec: config.routeTimeoutSec,
  });

  await appendEvent({
    source: "agent:manager",
    kind: "manager.decision",
    threadId: item.threadId,
    workItemId,
    payload: { ok: planning.ok, durationMs: planning.durationMs },
  });

  if (!planning.ok) {
    const text = `manager planning failed: ${planning.error ?? "unknown"}`;
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId,
      payload: { text },
    });
    return text;
  }

  const decision = extractJson<ManagerDecision>(planning.text);

  // No execution needed: manager answers in-thread directly.
  if (decision && (decision.instructions === null || decision.instructions === undefined) && decision.reply) {
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId,
      payload: { text: decision.reply },
    });
    return decision.reply;
  }

  const instructions = decision?.instructions ?? message;
  const executionId = genId("ex", 6);

  await appendEvent({
    source: "agent:manager",
    kind: "execution.started",
    threadId: item.threadId,
    workItemId,
    executionId,
    payload: { instructions, expect: decision?.expect ?? null },
  });

  const run = await runPi({
    prompt: instructions,
    charter: WORKER_CHARTER,
    cwd: item.workspace,
    tools: true,
    skills: true,
    thinking: config.workerThinking,
    timeoutSec: config.workerTimeoutSec,
    sessionDir: join(item.workspace, ".hidane", "sessions"),
  });

  await appendEvent({
    source: "agent:worker",
    kind: "execution.finished",
    threadId: item.threadId,
    workItemId,
    executionId,
    payload: {
      ok: run.ok,
      durationMs: run.durationMs,
      summary: run.text.slice(0, 8000),
      error: run.error ?? null,
    },
  });

  const replyText = run.ok
    ? run.text
    : `execution failed: ${run.error ?? "unknown"}\n${run.text}`;

  await appendEvent({
    source: "agent:manager",
    kind: "agent.reply",
    threadId: item.threadId,
    workItemId,
    executionId,
    payload: { text: replyText.slice(0, 8000) },
  });

  return replyText;
}
