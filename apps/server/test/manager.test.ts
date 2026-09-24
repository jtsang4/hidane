import { beforeEach, describe, expect, it, vi } from "vitest";
import { readFile } from "node:fs/promises";
import { join } from "node:path";

const stubs = vi.hoisted(() => ({
  promptRole: vi.fn(),
  steer: vi.fn(async () => true),
}));

vi.mock("../src/agents/sdk.js", () => ({
  getPrimarySession: vi.fn(async () => ({})),
  getManagerSession: vi.fn(async () => ({})),
  promptRole: stubs.promptRole,
}));
vi.mock("../src/agents/distiller.js", () => ({
  recallForPrimary: vi.fn(async () => ""),
  recallForManager: vi.fn(async () => ""),
}));
vi.mock("../src/agents/rpcWorker.js", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/agents/rpcWorker.js")>()),
  steerActiveWorker: stubs.steer,
}));

import { Runtime } from "../src/kernel/runtime.js";
import { listEvents } from "../src/kernel/events.js";
import { pendingMessages } from "../src/kernel/mailbox.js";
import { activeExecutionFor, getExecution } from "../src/kernel/executions.js";
import { createWorkItem, getWorkItem, listChildren } from "../src/kernel/workItems.js";
import { managerTurn } from "../src/agents/manager.js";
import { primaryTurn } from "../src/agents/primary.js";
import { submitMessage } from "../src/agents/ingress.js";
import { pumpExecutions, setWorkerRunner } from "../src/agents/workerPool.js";
import type { WorkerRunResult } from "../src/agents/rpcWorker.js";

function answer(effects: unknown[]) {
  stubs.promptRole.mockResolvedValueOnce({ ok: true, text: JSON.stringify({ effects }), durationMs: 1 });
}

function runtime(): Runtime {
  const rt = new Runtime();
  rt.register("primary", primaryTurn);
  rt.register("manager:", managerTurn);
  return rt;
}

/** A worker the test finishes by hand, so "the turn did not wait" is observable. */
function manualWorker() {
  const finishers: ((r: WorkerRunResult) => void)[] = [];
  const calls: { instructions: string; policyFiles: string[] }[] = [];
  setWorkerRunner(async (opts) => {
    calls.push({ instructions: opts.instructions, policyFiles: opts.policyFiles ?? [] });
    return new Promise<WorkerRunResult>((resolve) => finishers.push(resolve));
  });
  return {
    calls,
    finish: (r: Partial<WorkerRunResult> = {}) =>
      finishers.shift()!({ ok: true, text: "done", durationMs: 5, toolCalls: 1, ...r }),
  };
}

async function settle(): Promise<void> {
  for (let i = 0; i < 20; i++) {
    await pumpExecutions();
    await new Promise((r) => setTimeout(r, 10));
  }
}

describe("manager turn", () => {
  beforeEach(() => {
    stubs.promptRole.mockReset();
    stubs.steer.mockClear();
  });

  it("dispatches a worker and ends the turn; the result comes back as a message", async () => {
    const worker = manualWorker();
    const item = await createWorkItem("写脚本");
    const said = await submitMessage({ text: "写个 hello.py", source: "test", target: item.id });
    answer([
      { type: "understanding", text: "写一个打印 hello 的 Python 脚本" },
      { type: "spawn", instructions: "create hello.py", expect: "hello.py prints hello" },
    ]);
    const rt = runtime();
    await rt.drain();
    await settle();

    // The turn finished while the worker is still running.
    expect(worker.calls).toHaveLength(1);
    expect((await activeExecutionFor(item.id))?.status).toBe("running");
    const started = await listEvents({ kind: "execution.started" });
    expect(started[0]!.payload["root"]).toBe(said.id);
    expect(await readFile(join(item.workspace, "TASK.md"), "utf8")).toContain("hello");
    const [understanding] = await listEvents({ kind: "work_item.understanding" });
    expect(understanding!.payload["text"]).toContain("Python");

    worker.finish({ text: "created hello.py" });
    await settle();
    const [finished] = await pendingMessages(`manager:${item.id}`);
    expect(finished!.kind).toBe("execution.finished");
    expect(finished!.payload).toMatchObject({ ok: true, root: said.id });

    answer([{ type: "reply", reply: "写好了：hello.py" }]);
    await rt.drain();
    const replies = await listEvents({ kind: "agent.reply" });
    expect(replies.at(-1)!.payload).toMatchObject({ text: "写好了：hello.py", root: said.id });
    expect((await getExecution(started[0]!.executionId!))?.status).toBe("done");
  });

  it("steers the person's words into a running worker without waking the model", async () => {
    const worker = manualWorker();
    const item = await createWorkItem("爬虫");
    await submitMessage({ text: "写爬虫", source: "test", target: item.id });
    answer([{ type: "spawn", instructions: "write a crawler" }]);
    const rt = runtime();
    await rt.drain();
    await settle();
    await submitMessage({ text: "改用 Python", source: "test", target: item.id });
    await rt.drain();

    expect(stubs.promptRole).toHaveBeenCalledTimes(1);
    expect(stubs.steer).toHaveBeenCalledWith(item.id, "改用 Python");
    const [steered] = await listEvents({ kind: "execution.steered" });
    expect(steered!.payload["text"]).toBe("改用 Python");
    worker.finish();
    await settle();
  });

  it("acknowledges a cancelled run by rule", async () => {
    const worker = manualWorker();
    const item = await createWorkItem("长任务");
    await submitMessage({ text: "跑一下", source: "test", target: item.id });
    answer([{ type: "spawn", instructions: "run" }]);
    const rt = runtime();
    await rt.drain();
    await settle();
    worker.finish({ ok: false, cancelled: true, error: "cancelled", text: "" });
    await settle();
    await rt.drain();
    expect(stubs.promptRole).toHaveBeenCalledTimes(1);
    expect((await listEvents({ kind: "agent.reply" })).at(-1)!.payload["text"]).toBe("执行已取消。");
  });

  it("bubbles a question from a child through its parent to the person", async () => {
    const parent = await createWorkItem("调研向量库");
    const said = await submitMessage({ text: "调研三个向量库", source: "test", target: parent.id });
    answer([
      {
        type: "create_children",
        children: [
          { title: "调研 A", brief: "look at A" },
          { title: "调研 B", brief: "look at B" },
        ],
      },
    ]);
    const rt = runtime();
    // Only the parent turn: the children's first messages stay queued.
    await rt.step();
    const children = await listChildren(parent.id);
    expect(children.map((c) => c.title)).toEqual(["调研 A", "调研 B"]);

    // Child A cannot proceed; child B has nothing to do this turn.
    stubs.promptRole.mockImplementation(async (_s: unknown, prompt: string) => {
      if (prompt.includes("look at A")) {
        return {
          ok: true,
          text: JSON.stringify({ effects: [{ type: "escalate", question: "需要 A 的账号", tried: "查了文档" }] }),
          durationMs: 1,
        };
      }
      if (prompt.includes("需要 A 的账号")) {
        return {
          ok: true,
          text: JSON.stringify({ effects: [{ type: "escalate", question: "需要 A 的账号", tried: "父任务没有账号" }] }),
          durationMs: 1,
        };
      }
      return { ok: true, text: JSON.stringify({ effects: [] }), durationMs: 1 };
    });
    await rt.drain();

    const [escalation] = await listEvents({ kind: "escalation" });
    expect(escalation!.workItemId).toBe(parent.id);
    expect(escalation!.payload["root"]).toBe(said.id);
    const path = escalation!.payload["path"] as { workItemId: string; tried: string }[];
    expect(path.map((p) => p.workItemId)).toEqual([children[0]!.id, parent.id]);
    expect(path[0]!.tried).toBe("查了文档");
  });

  it("tells the parent once all children have settled", async () => {
    const parent = await createWorkItem("parent");
    const a = await createWorkItem("A", "test", { parentId: parent.id });
    const b = await createWorkItem("B", "test", { parentId: parent.id });
    await submitMessage({ text: "go A", source: "test", target: a.id });
    await submitMessage({ text: "go B", source: "test", target: b.id });
    // The two children run concurrently, so answer by content, not by order.
    stubs.promptRole.mockImplementation(async (_s: unknown, prompt: string) => ({
      ok: true,
      text: JSON.stringify({
        effects: [{ type: "reply", reply: prompt.includes("go A") ? "A 的结果" : "B 的结果" }, { type: "done" }],
      }),
      durationMs: 1,
    }));
    const rt = runtime();
    await rt.step();
    const [settled] = await pendingMessages(`manager:${parent.id}`);
    expect(settled!.kind).toBe("children.settled");
    const results = settled!.payload["children"] as { title: string; status: string; result: string }[];
    expect(results.map((r) => [r.title, r.status, r.result])).toEqual([
      ["A", "done", "A 的结果"],
      ["B", "done", "B 的结果"],
    ]);
  });

  it("sends a message that is not its own back up for rerouting", async () => {
    const item = await createWorkItem("登录页样式");
    const said = await submitMessage({ text: "查下昨天的报错", source: "test", target: item.id });
    const [forwarded] = await pendingMessages(`manager:${item.id}`);
    answer([{ type: "reroute", of: forwarded!.id }]);
    await runtime().step();
    const [request] = await pendingMessages("primary");
    expect(request!.kind).toBe("message.reroute_requested");
    expect(request!.payload).toMatchObject({ of: said.id, exclude: [item.id] });
  });

  it("stops spawning once the item's execution budget is spent", async () => {
    const worker = manualWorker();
    const item = await createWorkItem("budget");
    const { config } = await import("../src/config.js");
    const before = config.maxExecutionsPerItem;
    config.maxExecutionsPerItem = 1;
    try {
      const rt = runtime();
      await submitMessage({ text: "one", source: "test", target: item.id });
      answer([{ type: "spawn", instructions: "first" }]);
      await rt.drain();
      await settle();
      worker.finish();
      await settle();
      answer([{ type: "spawn", instructions: "second" }]);
      await rt.drain();
      expect(worker.calls).toHaveLength(1);
      const [escalation] = await listEvents({ kind: "escalation" });
      expect(escalation!.payload["reason"]).toBe("budget");
    } finally {
      config.maxExecutionsPerItem = before;
    }
  });

  it("passes every policy file from the global one down to the worker", async () => {
    const worker = manualWorker();
    const parent = await createWorkItem("parent");
    const child = await createWorkItem("child", "test", { parentId: parent.id });
    await submitMessage({ text: "go", source: "test", target: child.id });
    answer([{ type: "spawn", instructions: "work" }]);
    await runtime().drain();
    await settle();
    const files = worker.calls[0]!.policyFiles;
    expect(files).toHaveLength(3);
    expect(files[0]).toMatch(/POLICY\.json$/);
    expect(files[1]).toContain((await getWorkItem(parent.id)).workspace);
    expect(files[2]).toContain(child.workspace);
    worker.finish();
    await settle();
  });

  it("does not let a turn end with no response to the person", async () => {
    const item = await createWorkItem("部署");
    await submitMessage({ text: "部署到我的服务器", source: "test", target: item.id });
    answer([{ type: "understanding", text: "部署，但缺服务器地址" }]);
    answer([{ type: "escalate", question: "服务器地址是？", tried: "工作区里没有配置" }]);
    await runtime().drain();
    expect(stubs.promptRole).toHaveBeenCalledTimes(2);
    const [escalation] = await listEvents({ kind: "escalation" });
    expect(escalation!.payload["question"]).toBe("服务器地址是？");
    expect(await listEvents({ kind: "work_item.understanding" })).toHaveLength(1);
  });

  it("records a refused tool call with the rule's own words", async () => {
    const worker = manualWorker();
    const item = await createWorkItem("清理");
    await submitMessage({ text: "删掉 a.md", source: "test", target: item.id });
    answer([{ type: "spawn", instructions: "rm a.md" }]);
    await runtime().drain();
    await settle();
    worker.finish({ policyBlocks: [{ tool: "bash", reason: "blocked by hidane policy pol_x: 不允许删除文件" }] });
    await settle();
    const [blocked] = await listEvents({ kind: "policy.blocked" });
    expect(blocked!.payload).toMatchObject({ tool: "bash", rule: "pol_x", reason: "不允许删除文件" });
  });
});
