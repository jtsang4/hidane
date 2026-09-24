import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { config } from "../config.js";
import { appendEvent, getEvent, listEvents, type HidaneEvent } from "../kernel/events.js";
import { post, rootOf } from "../kernel/mailbox.js";
import { activeExecutionFor, countExecutions } from "../kernel/executions.js";
import { bubbles } from "../kernel/propagation.js";
import {
  createWorkItem,
  getWorkItem,
  listChildren,
  type WorkItem,
} from "../kernel/workItems.js";
import { MANAGER_CHARTER } from "./charters.js";
import { getManagerSession } from "./sdk.js";
import { recallForManager } from "./distiller.js";
import { loadImages, storedImagesOf } from "./inbox.js";
import { nowLine, str, think, type Effect } from "./think.js";
import { changeStatus, parentMailbox } from "./ingress.js";
import { managerAddress, workItemIdOf } from "./addresses.js";
import { steerActiveWorker } from "./rpcWorker.js";
import { amendQueued, dispatchExecution } from "./workerPool.js";

function describe(m: HidaneEvent, children: WorkItem[]): string {
  const p = m.payload;
  switch (m.kind) {
    case "user.message":
      return `[${m.id}] (person${p["answers"] ? ", answering your escalated question" : ""}${p["deferred"] ? ", recorded earlier" : ""}) ${String(p["text"] ?? "")}`;
    case "execution.finished": {
      const status = p["cancelled"]
        ? "cancelled"
        : p["lost"]
          ? "lost (runtime restarted)"
          : p["ok"]
            ? p["blocked"]
              ? "blocked"
              : "ok"
            : "failed";
      return [
        `[${m.id}] (worker result ${m.executionId ?? ""}: ${status})`,
        p["blocked"] ? `blocked on: ${String(p["blocked"])}` : "",
        p["error"] ? `error: ${String(p["error"]).slice(0, 1000)}` : "",
        Array.isArray(p["policyBlocks"]) && p["policyBlocks"].length > 0
          ? `refused by policy: ${(p["policyBlocks"] as { reason: string }[]).map((b) => b.reason).join("; ")}`
          : "",
        `summary:\n${String(p["summary"] ?? "").slice(0, 6000)}`,
      ]
        .filter(Boolean)
        .join("\n");
    }
    case "escalation.raised": {
      const child = children.find((c) => c.id === m.workItemId);
      return `[${m.id}] (question escalated by child ${m.workItemId ?? ""}${child ? ` "${child.title}"` : ""}) ${String(p["question"] ?? "")}`;
    }
    case "children.settled":
      return `[${m.id}] (all child work items finished)\n${(p["children"] as { workItemId: string; title: string; status: string; result: string }[])
        .map((c) => `- ${c.workItemId} "${c.title}" [${c.status}]: ${c.result}`)
        .join("\n")}`;
    case "message.reroute_requested":
      return `[${m.id}] (child ${m.workItemId ?? ""} says this message is not theirs) ${String(p["text"] ?? "")}`;
    default:
      return `[${m.id}] (${m.kind}) ${JSON.stringify(p).slice(0, 500)}`;
  }
}

/** The latest message decides the chain position of everything this turn emits. */
function latest(messages: HidaneEvent[]): HidaneEvent {
  return messages.reduce((a, b) => (b.hop > a.hop ? b : a));
}

class ManagerContext {
  constructor(
    readonly item: WorkItem,
    readonly batch: HidaneEvent[],
    readonly cause: HidaneEvent,
  ) {}

  async reply(text: string, of?: HidaneEvent): Promise<void> {
    const anchor = of ?? this.cause;
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: this.item.threadId,
      workItemId: this.item.id,
      causedBy: anchor.id,
      hop: anchor.hop + 1,
      payload: {
        text: text.slice(0, 8000),
        of: anchor.id,
        root: rootOf(anchor),
        // A child's answer is for its parent; the person reads the parent's summary.
        ...(this.item.parentId ? { child: true } : {}),
      },
    });
  }

  /** Send a fact up one level of the work tree. */
  async bubble(kind: string, payload: Record<string, unknown>): Promise<void> {
    if (!bubbles(kind)) throw new Error(`${kind} does not bubble`);
    await post({
      source: "agent:manager",
      kind,
      mailbox: parentMailbox(this.item),
      lane: "normal",
      threadId: this.item.threadId,
      workItemId: this.item.id,
      causedBy: this.cause,
      payload: { root: rootOf(this.cause), ...payload },
    });
  }
}

async function applyEffect(ctx: ManagerContext, e: Effect, spawned: { done: boolean }): Promise<void> {
  const { item } = ctx;
  switch (e.type) {
    case "understanding": {
      const text = str(e["text"]);
      if (!text) return;
      // The file is what agents read (workers see it in their cwd); the event
      // is what the card shows. Neither is derived from the other.
      await mkdir(item.workspace, { recursive: true });
      await writeFile(join(item.workspace, "TASK.md"), `# ${item.title}\n\n${text}\n`);
      await appendEvent({
        source: "agent:manager",
        kind: "work_item.understanding",
        threadId: item.threadId,
        workItemId: item.id,
        causedBy: ctx.cause.id,
        payload: { text, root: rootOf(ctx.cause) },
      });
      return;
    }
    case "reply": {
      const of = typeof e["of"] === "string" ? ctx.batch.find((m) => m.id === e["of"]) : undefined;
      await ctx.reply(str(e["reply"]) ?? "", of);
      return;
    }
    case "spawn": {
      const instructions = str(e["instructions"]);
      if (!instructions || spawned.done) return;
      spawned.done = true;
      if ((await countExecutions(item.id)) >= config.maxExecutionsPerItem) {
        await appendEvent({
          source: "agent:manager",
          kind: "escalation",
          threadId: "main",
          workItemId: item.id,
          causedBy: ctx.cause.id,
          payload: {
            reason: "budget",
            question: `「${item.title}」已经执行了 ${config.maxExecutionsPerItem} 次，已暂停。需要继续的话，直接回复这个任务。`,
            root: rootOf(ctx.cause),
          },
        });
        return;
      }
      await dispatchExecution({
        item,
        owner: managerAddress(item.id),
        instructions,
        expect: str(e["expect"]),
        causedBy: ctx.cause,
      });
      return;
    }
    case "escalate": {
      const question = str(e["question"]);
      if (!question) return;
      const incoming = ctx.batch.find((m) => m.kind === "escalation.raised");
      const path = [
        ...((incoming?.payload["path"] as unknown[] | undefined) ?? []),
        { workItemId: item.id, title: item.title, tried: str(e["tried"]) ?? "" },
      ];
      await ctx.bubble("escalation.raised", { question, path });
      return;
    }
    case "answer": {
      const childId = str(e["work_item_id"]);
      const text = str(e["text"]);
      if (!childId || !text) return;
      const child = (await listChildren(item.id)).find((c) => c.id === childId);
      if (!child) return;
      await post({
        source: "agent:manager",
        kind: "user.message",
        mailbox: managerAddress(child.id),
        lane: "interrupt",
        threadId: child.threadId,
        workItemId: child.id,
        causedBy: ctx.cause,
        payload: { text, root: rootOf(ctx.cause), fromParent: item.id },
      });
      return;
    }
    case "create_children": {
      const children = Array.isArray(e["children"]) ? e["children"] : [];
      for (const raw of children.slice(0, 8)) {
        const title = str((raw as Record<string, unknown>)["title"]);
        const brief = str((raw as Record<string, unknown>)["brief"]);
        if (!title || !brief) continue;
        const child = await createWorkItem(title, "agent:manager", { parentId: item.id });
        await post({
          source: "agent:manager",
          kind: "user.message",
          mailbox: managerAddress(child.id),
          lane: "normal",
          threadId: child.threadId,
          workItemId: child.id,
          causedBy: ctx.cause,
          payload: { text: brief, root: rootOf(ctx.cause), fromParent: item.id },
        });
      }
      return;
    }
    case "reroute": {
      const of = typeof e["of"] === "string" ? ctx.batch.find((m) => m.id === e["of"]) : undefined;
      const message = of ?? ctx.batch.find((m) => m.kind === "user.message");
      if (!message) return;
      await ctx.bubble("message.reroute_requested", {
        of: String(message.payload["of"] ?? message.id),
        text: message.payload["text"],
        exclude: [item.id],
      });
      return;
    }
    case "done": {
      if (!item.parentId) return;
      await changeStatus(item.id, "done", "agent:manager", ctx.cause);
      return;
    }
    default:
      return;
  }
}

/**
 * A Manager's turn. Rules first: while its worker is running, the person's
 * words are steered straight into that worker (the model is not woken for
 * it), and a cancelled run is acknowledged without a model call. Everything
 * else — refinements merged across the batch, worker results, children's
 * questions — is one model call that ends by dispatching, never by waiting.
 */
export async function managerTurn(address: string, messages: HidaneEvent[]): Promise<void> {
  const item = await getWorkItem(workItemIdOf(address));
  const active = await activeExecutionFor(item.id);
  const personal = messages.filter((m) => m.kind === "user.message");
  const others = messages.filter((m) => m.kind !== "user.message");

  if (active && others.length === 0 && personal.length > 0) {
    for (const m of personal) {
      const text = String(m.payload["text"] ?? "");
      const steered =
        active.status === "running"
          ? await steerActiveWorker(item.id, text)
          : amendQueued(item.id, text);
      await appendEvent({
        source: "agent:manager",
        kind: steered ? "execution.steered" : "agent.error",
        threadId: item.threadId,
        workItemId: item.id,
        executionId: active.id,
        causedBy: m.id,
        payload: steered
          ? { text, of: m.id, root: rootOf(m), queued: active.status === "queued" }
          : { error: "could not deliver the message to the running execution", of: m.id, root: rootOf(m) },
      });
    }
    return;
  }

  const onlyCancelled =
    personal.length === 0 &&
    others.length > 0 &&
    others.every((m) => m.kind === "execution.finished" && m.payload["cancelled"] === true);
  const cause = latest(messages);
  if (onlyCancelled) {
    await new ManagerContext(item, messages, cause).reply("执行已取消。");
    return;
  }

  const [children, memories, history] = await Promise.all([
    listChildren(item.id),
    recallForManager(item.id),
    listEvents({ workItemId: item.id, tail: 40 }),
  ]);
  const inBatch = new Set(messages.map((m) => m.id));
  const historyText = history
    .filter((e) => !inBatch.has(e.id))
    .filter((e) =>
      (e.kind === "user.message" && e.threadId !== "main") ||
      e.kind === "agent.reply" ||
      e.kind === "work_item.understanding",
    )
    .slice(-14)
    .map((e) => `[${e.kind}] ${String(e.payload["text"] ?? "").slice(0, 600)}`)
    .join("\n");
  const parent = item.parentId ? await getWorkItem(item.parentId).catch(() => undefined) : undefined;
  const escalatedFrom = messages.find((m) => m.kind === "user.message" && m.payload["answers"]);
  const answered = escalatedFrom
    ? await getEvent(String(escalatedFrom.payload["answers"]))
    : undefined;

  const session = await getManagerSession(
    item.id,
    item.workspace,
    join(item.workspace, ".hidane", "sessions", "manager"),
    MANAGER_CHARTER,
  );
  const images = await loadImages(messages.flatMap((m) => storedImagesOf(m.payload)));
  const thought = await think(
    session,
    [
      nowLine(),
      memories,
      `Work item: ${item.id} — ${item.title} (status: ${item.status})`,
      parent ? `Parent work item: ${parent.id} — ${parent.title}. You are a CHILD work item.` : "",
      children.length > 0
        ? `Child work items:\n${children.map((c) => `- ${c.id} [${c.status}] ${c.title}`).join("\n")}`
        : "",
      `Workspace: ${item.workspace}`,
      active ? `An execution (${active.id}) is ${active.status}; do not spawn another until it reports.` : "",
      answered ? `Question you escalated earlier: ${String(answered.payload["question"] ?? "")}` : "",
      historyText ? `Earlier in this work item:\n${historyText}` : "",
      `Messages this turn:\n${messages.map((m) => describe(m, children)).join("\n\n")}`,
    ]
      .filter(Boolean)
      .join("\n\n"),
    { images, liveThreadId: item.threadId },
  );

  await appendEvent({
    source: "agent:manager",
    kind: "manager.decision",
    threadId: item.threadId,
    workItemId: item.id,
    causedBy: cause.id,
    payload: {
      ok: thought.ok,
      durationMs: thought.durationMs,
      of: messages.map((m) => m.id),
      effects: (thought.effects ?? []) as unknown[],
    },
  });

  const ctx = new ManagerContext(item, messages, cause);
  if (!thought.ok) {
    await ctx.reply(`manager planning failed: ${thought.error ?? "unknown"}`);
    return;
  }
  if (!thought.effects) {
    // Not the effect JSON: the text itself is the Manager's answer.
    if (thought.raw.trim()) await ctx.reply(thought.raw);
    return;
  }
  let effects = thought.effects;
  // A turn that only restates its understanding leaves the person waiting on a
  // task that silently went idle. Ask once for the missing decision.
  if (!effects.some((e) => ACTIONS.has(e.type))) {
    const retry = await think(
      session,
      "Your answer contained no action, so the person would get no response. Respond again with the full effect list, including at least one of: spawn, reply, escalate, create_children, reroute, answer, done.",
      { liveThreadId: item.threadId },
    );
    if (retry.ok && retry.effects && retry.effects.some((e) => ACTIONS.has(e.type))) {
      effects = [...effects, ...retry.effects.filter((e) => e.type !== "understanding")];
    }
  }
  const spawned = { done: false };
  for (const effect of effects) {
    await applyEffect(ctx, effect, spawned);
  }
}

/** Effects that move a work item forward or answer someone. */
const ACTIONS = new Set(["spawn", "reply", "escalate", "create_children", "reroute", "answer", "done"]);

