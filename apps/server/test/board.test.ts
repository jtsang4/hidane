import { describe, expect, it } from "vitest";
import { appendEvent } from "../src/kernel/events.js";
import { post } from "../src/kernel/mailbox.js";
import { createExecution, setExecutionStatus } from "../src/kernel/executions.js";
import { createWorkItem, setWorkItemStatus } from "../src/kernel/workItems.js";
import { buildBoard } from "../src/projections/board.js";
import { buildApp } from "../src/connectors/http.js";

describe("board projection", () => {
  it("derives each card's state from the log and state tables", async () => {
    const idle = await createWorkItem("idle", "test", { of: "ev_anchor" });
    const running = await createWorkItem("running");
    const waiting = await createWorkItem("waiting");
    const thinking = await createWorkItem("thinking");
    const done = await createWorkItem("done");
    const parent = await createWorkItem("parent");
    await createWorkItem("child", "test", { parentId: parent.id });
    await setWorkItemStatus(done.id, "done");

    await createExecution("ex_run", running.id, `manager:${running.id}`);
    await setExecutionStatus("ex_run", "running");
    await appendEvent({
      source: "agent:manager",
      kind: "execution.started",
      workItemId: running.id,
      executionId: "ex_run",
      payload: { instructions: "build it" },
    });
    await appendEvent({
      source: "agent:worker",
      kind: "side_effect.intent",
      workItemId: running.id,
      executionId: "ex_run",
      payload: { tool: "bash", input: "ls" },
    });
    await appendEvent({
      source: "agent:manager",
      kind: "work_item.understanding",
      workItemId: running.id,
      payload: { text: "构建项目" },
    });
    await appendEvent({
      source: "agent:primary",
      kind: "escalation",
      threadId: "main",
      workItemId: waiting.id,
      payload: { question: "要哪个账号？", path: [] },
    });
    await post({ source: "test", kind: "user.message", mailbox: `manager:${thinking.id}`, workItemId: thinking.id });

    const cards = await buildBoard();
    const state = Object.fromEntries(cards.map((c) => [c.item.title, c.state]));
    expect(state).toEqual({
      idle: "idle",
      running: "running",
      waiting: "waiting",
      thinking: "thinking",
      done: "done",
      parent: "delegated",
      child: "idle",
    });
    const run = cards.find((c) => c.item.id === running.id)!;
    expect(run.understanding).toBe("构建项目");
    expect(run.execution).toMatchObject({ id: "ex_run", instructions: "build it", toolCalls: 1 });
    expect(run.execution?.lastTool).toContain("bash");
    expect(cards.find((c) => c.item.id === waiting.id)!.escalation?.question).toBe("要哪个账号？");
    expect(cards.find((c) => c.item.id === idle.id)!.anchor).toBe("ev_anchor");
  });

  it("closes an escalation once the person has answered the item", async () => {
    const item = await createWorkItem("asks");
    await appendEvent({
      source: "agent:primary",
      kind: "escalation",
      threadId: "main",
      workItemId: item.id,
      payload: { question: "?" },
    });
    await appendEvent({
      source: "connector:web",
      kind: "user.message",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: "用这个" },
    });
    const [card] = await buildBoard();
    expect(card!.escalation).toBeNull();
  });

  it("does not mistake an old completion marker for a question", async () => {
    const item = await createWorkItem("legacy");
    await appendEvent({
      source: "agent:manager",
      kind: "escalation",
      threadId: "main",
      workItemId: item.id,
      payload: { note: "work item finished a run" },
    });
    const [card] = await buildBoard();
    expect(card!.state).toBe("idle");
    expect(card!.escalation).toBeNull();
  });

  it("is served at /api/board", async () => {
    await createWorkItem("served");
    const res = await buildApp().request("/api/board");
    const body = (await res.json()) as { cards: { item: { title: string } }[] };
    expect(body.cards.map((c) => c.item.title)).toEqual(["served"]);
  });
});

describe("tool descriptions", () => {
  it("show the command or path, not the raw JSON", async () => {
    const { describeTool } = await import("../src/projections/board.js");
    expect(describeTool({ tool: "bash", input: JSON.stringify({ command: "cd /w && ls -la" }) })).toBe("bash cd /w && ls -la");
    expect(describeTool({ tool: "write", input: JSON.stringify({ path: "login.html", content: "…" }) })).toBe("write login.html");
    expect(describeTool({ tool: "bash", input: '{"command":"echo cut' })).toBe('bash {"command":"echo cut');
  });
});
