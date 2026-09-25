import { beforeEach, describe, expect, it, vi } from "vitest";

const stubs = vi.hoisted(() => ({
  promptRole: vi.fn(),
}));

vi.mock("../src/agents/sdk.js", () => ({
  openPrimarySession: vi.fn(async () => ({ dispose: vi.fn() })),
  getManagerSession: vi.fn(async () => ({})),
  promptRole: stubs.promptRole,
}));
vi.mock("../src/agents/distiller.js", () => ({
  recallForPrimary: vi.fn(async () => ""),
  recallForManager: vi.fn(async () => ""),
}));

import { Runtime } from "../src/kernel/runtime.js";
import { listEvents } from "../src/kernel/events.js";
import { pendingMessages, post } from "../src/kernel/mailbox.js";
import { createExecution } from "../src/kernel/executions.js";
import { createWorkItem, getWorkItem, listWorkItems, setWorkItemStatus } from "../src/kernel/workItems.js";
import { primaryTurn } from "../src/agents/primary.js";
import { submitMessage } from "../src/agents/ingress.js";

function answer(effects: unknown[]) {
  stubs.promptRole.mockResolvedValueOnce({ ok: true, text: JSON.stringify({ effects }), durationMs: 1 });
}

function primaryOnly(): Runtime {
  const runtime = new Runtime();
  runtime.register("primary", primaryTurn);
  return runtime;
}

async function say(text: string) {
  return submitMessage({ text, source: "connector:web" });
}

describe("primary turn", () => {
  beforeEach(() => {
    stubs.promptRole.mockReset();
  });

  it("decides everything that piled up in one model call", async () => {
    const a = await say("帮我写个爬虫");
    const b = await say("用 Python");
    const c = await say("结果存成 CSV");
    answer([
      {
        type: "create_work_item",
        of: a.id,
        also_of: [b.id, c.id],
        title: "写爬虫",
        brief: "用 Python 写爬虫，输出 CSV",
        repo: null,
        dispatch: true,
      },
    ]);
    await primaryOnly().drain();

    expect(stubs.promptRole).toHaveBeenCalledTimes(1);
    const prompt = String(stubs.promptRole.mock.calls[0]![1]);
    for (const m of [a, b, c]) expect(prompt).toContain(`[${m.id}]`);
    expect(prompt).toContain(`Now: `);
    expect(prompt).toContain(String(new Date().getFullYear()));
    const items = await listWorkItems();
    expect(items).toHaveLength(1);
    // Every refinement reaches the Manager; none is dropped by the merge.
    const inbox = await pendingMessages(`manager:${items[0]!.id}`);
    expect(inbox.map((e) => e.payload["text"])).toEqual([
      "用 Python 写爬虫，输出 CSV",
      "用 Python",
      "结果存成 CSV",
    ]);
    const attributed = await listEvents({ kind: "message.attributed" });
    expect(attributed.map((e) => e.payload["of"])).toEqual([a.id, b.id, c.id]);
    expect(attributed[0]!.payload["created"]).toBe(true);
    const created = await listEvents({ kind: "work_item.created" });
    expect(created[0]!.payload["of"]).toBe(a.id);
    expect(await pendingMessages("primary")).toHaveLength(0);
  });

  it("answers directly with the reply anchored to the message it answers", async () => {
    const m = await say("你好");
    answer([{ type: "reply", of: m.id, reply: "你好！" }]);
    await primaryOnly().drain();
    const [reply] = await listEvents({ kind: "agent.reply" });
    expect(reply!.payload).toMatchObject({ text: "你好！", of: m.id, root: m.id });
  });

  it("routes when confident and asks when it is not", async () => {
    const style = await createWorkItem("登录页样式");
    const chart = await createWorkItem("周报图表");
    const sure = await say("登录按钮再圆一点");
    const unsure = await say("颜色再深一点");
    answer([
      { type: "route", of: sure.id, work_item_id: style.id, confidence: 0.95 },
      { type: "route", of: unsure.id, work_item_id: chart.id, confidence: 0.3 },
    ]);
    await primaryOnly().drain();

    expect(await pendingMessages(`manager:${style.id}`)).toHaveLength(1);
    // Not confident enough: no guess is delivered anywhere.
    expect(await pendingMessages(`manager:${chart.id}`)).toHaveLength(0);
    const [ambiguous] = await listEvents({ kind: "attribution.ambiguous" });
    expect(ambiguous!.payload["of"]).toBe(unsure.id);
    expect(ambiguous!.payload["candidates"]).toEqual([
      { workItemId: chart.id, title: "周报图表" },
      { workItemId: "new", title: "" },
    ]);
  });

  it("puts an explicit choice in front of the person", async () => {
    const one = await createWorkItem("方案一");
    const two = await createWorkItem("方案二");
    const m = await say("这个再改改");
    answer([{ type: "ambiguous", of: m.id, candidates: [one.id, two.id, "wi_invented"], reply: "哪一个？" }]);
    await primaryOnly().drain();
    const [ambiguous] = await listEvents({ kind: "attribution.ambiguous" });
    expect(ambiguous!.payload["question"]).toBe("哪一个？");
    expect((ambiguous!.payload["candidates"] as { workItemId: string }[]).map((c) => c.workItemId)).toEqual([
      one.id,
      two.id,
    ]);
  });

  it("closes all open work items as a direct state operation", async () => {
    const first = await createWorkItem("First open item");
    const second = await createWorkItem("Second open item");
    const m = await say("把所有已有工作项关停");
    answer([{ type: "set_status", of: m.id, all_open: true, status: "closed" }]);
    await primaryOnly().drain();

    expect(await listWorkItems("open")).toHaveLength(0);
    expect((await getWorkItem(first.id)).status).toBe("closed");
    expect((await getWorkItem(second.id)).status).toBe("closed");
    const changes = await listEvents({ kind: "work_item.status_changed" });
    expect(changes).toHaveLength(2);
    expect(changes.every((event) => event.source === "agent:primary")).toBe(true);
    const [reply] = await listEvents({ kind: "agent.reply" });
    expect(reply!.payload["text"]).toContain("2 个工作项");
  });

  it("reopens an explicitly selected item", async () => {
    const item = await createWorkItem("Already done");
    await setWorkItemStatus(item.id, "done", "test");
    const m = await say("把它重新打开");
    answer([{ type: "set_status", of: m.id, work_item_ids: [item.id], status: "open" }]);
    await primaryOnly().drain();
    expect((await getWorkItem(item.id)).status).toBe("open");
  });

  it("records a deferred work item without starting its Manager", async () => {
    const m = await say("先记录这个工作项，不要开始");
    answer([{ type: "create_work_item", of: m.id, title: "Deferred", brief: "later", dispatch: false }]);
    await primaryOnly().drain();
    const [item] = await listWorkItems();
    expect(await pendingMessages(`manager:${item!.id}`)).toHaveLength(0);
    const deferred = await listEvents({ threadId: item!.threadId, kind: "user.message" });
    expect(deferred[0]!.payload["deferred"]).toBe(true);
    const [reply] = await listEvents({ kind: "agent.reply" });
    expect(reply!.payload["text"]).toContain("暂未启动");
  });

  it("cancels a work item and everything running under it", async () => {
    const parent = await createWorkItem("parent");
    const child = await createWorkItem("child", "test", { parentId: parent.id });
    await createExecution("ex_child", child.id, `manager:${child.id}`);
    const m = await say("停止这个任务");
    answer([{ type: "cancel", of: m.id, work_item_ids: [parent.id] }]);
    await primaryOnly().drain();
    const cancelled = await listEvents({ kind: "execution.cancelled" });
    expect(cancelled.map((e) => e.workItemId)).toEqual([child.id]);
    expect(cancelled[0]!.payload["via"]).toBe(parent.id);
    // The queued execution's owner is told, like any other outcome.
    const [finished] = await pendingMessages(`manager:${child.id}`);
    expect(finished?.kind).toBe("execution.finished");
    expect(finished?.payload["cancelled"]).toBe(true);
  });

  it("never leaves a message without an answer", async () => {
    const m = await say("嗯？");
    answer([]);
    await primaryOnly().drain();
    const [reply] = await listEvents({ kind: "agent.reply" });
    expect(reply!.payload["of"]).toBe(m.id);
  });

  it("hands a bubbled question to the person without waking the model", async () => {
    const item = await createWorkItem("needs a key");
    await post({
      source: "agent:manager",
      kind: "escalation.raised",
      mailbox: "primary",
      workItemId: item.id,
      payload: {
        question: "需要 OPENAI_KEY",
        path: [{ workItemId: item.id, title: item.title, tried: "查了项目配置" }],
        root: "ev_root",
      },
    });
    await primaryOnly().drain();
    expect(stubs.promptRole).not.toHaveBeenCalled();
    const [escalation] = await listEvents({ kind: "escalation" });
    expect(escalation!.threadId).toBe("main");
    expect(escalation!.workItemId).toBe(item.id);
    expect(escalation!.payload).toMatchObject({ question: "需要 OPENAI_KEY", root: "ev_root" });
  });

  it("reroutes a message a Manager refused, never back to that Manager", async () => {
    const wrong = await createWorkItem("wrong");
    const right = await createWorkItem("right");
    const original = await say("顺便查下昨天的报错");
    const request = await post({
      source: "agent:manager",
      kind: "message.reroute_requested",
      mailbox: "primary",
      workItemId: wrong.id,
      payload: { of: original.id, text: "顺便查下昨天的报错", exclude: [wrong.id] },
    });
    answer([
      { type: "reply", of: original.id, reply: "处理中" },
      { type: "route", of: request!.id, work_item_id: wrong.id, confidence: 1 },
      { type: "route", of: request!.id, work_item_id: right.id, confidence: 1 },
    ]);
    await primaryOnly().drain();
    expect(await pendingMessages(`manager:${wrong.id}`)).toHaveLength(0);
    const [delivered] = await pendingMessages(`manager:${right.id}`);
    expect(delivered!.payload["of"]).toBe(original.id);
  });
});
