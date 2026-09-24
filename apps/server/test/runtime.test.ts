import { describe, expect, it } from "vitest";
import { Runtime } from "../src/kernel/runtime.js";
import { appendEvent, getCursor, listEvents, type HidaneEvent } from "../src/kernel/events.js";
import { mailboxesWithPending, pendingMessages, post } from "../src/kernel/mailbox.js";
import { createExecution, getExecution } from "../src/kernel/executions.js";
import { createWorkItem, subtree } from "../src/kernel/workItems.js";
import { config } from "../src/config.js";
import { recoverExecutions } from "../src/agents/workerPool.js";
import { bubbles } from "../src/kernel/propagation.js";

describe("mailboxes", () => {
  it("are a view of the log: posting appends, the cursor marks what was read", async () => {
    const m = await post({ source: "test", kind: "note", mailbox: "a", payload: { n: 1 } });
    expect(m?.mailbox).toBe("a");
    expect(m?.lane).toBe("normal");
    expect((await pendingMessages("a")).map((e) => e.id)).toEqual([m!.id]);
    expect(await getCursor("mailbox:a")).toBe(m!.seq - 1);
  });

  it("list interrupt-lane mailboxes first, then the oldest", async () => {
    await post({ source: "test", kind: "note", mailbox: "old" });
    await post({ source: "test", kind: "note", mailbox: "newer" });
    await post({ source: "test", kind: "note", mailbox: "urgent", lane: "interrupt" });
    expect((await mailboxesWithPending()).map((m) => m.address)).toEqual(["urgent", "old", "newer"]);
  });

  it("stop a causal chain that runs too long and ask a person instead", async () => {
    const before = config.maxHops;
    config.maxHops = 2;
    try {
      let cause: HidaneEvent | null = await post({ source: "test", kind: "ping", mailbox: "x" });
      for (let i = 0; i < 2; i++) {
        cause = await post({ source: "test", kind: "ping", mailbox: "x", causedBy: cause! });
      }
      expect(cause?.hop).toBe(2);
      const refused = await post({ source: "test", kind: "ping", mailbox: "x", causedBy: cause! });
      expect(refused).toBeNull();
      const [escalation] = await listEvents({ kind: "escalation" });
      expect(escalation!.payload).toMatchObject({ reason: "budget", blockedKind: "ping" });
      expect(await pendingMessages("x")).toHaveLength(3);
    } finally {
      config.maxHops = before;
    }
  });
});

describe("runtime", () => {
  it("hands everything that piled up to one turn", async () => {
    const turns: string[][] = [];
    const rt = new Runtime();
    rt.register("box", async (_a, messages) => {
      turns.push(messages.map((m) => String(m.payload["text"])));
    });
    for (const text of ["帮我写个爬虫", "用 Python", "存成 CSV"]) {
      await post({ source: "test", kind: "user.message", mailbox: "box", payload: { text } });
    }
    await rt.drain();
    expect(turns).toEqual([["帮我写个爬虫", "用 Python", "存成 CSV"]]);
    expect(await pendingMessages("box")).toHaveLength(0);
  });

  it("serves an interrupt before normal work when turns are scarce", async () => {
    const order: string[] = [];
    const rt = new Runtime({ maxConcurrentTurns: 1 });
    const record = async (address: string) => {
      order.push(address);
    };
    rt.register("bg", record);
    rt.register("person", record);
    await post({ source: "test", kind: "wake", mailbox: "bg" });
    await post({ source: "test", kind: "said", mailbox: "person", lane: "interrupt" });
    await rt.drain();
    expect(order).toEqual(["person", "bg"]);
  });

  it("never runs two turns of one mailbox at once, and picks up what arrived meanwhile", async () => {
    let concurrent = 0;
    let max = 0;
    const seen: number[] = [];
    const rt = new Runtime();
    rt.register("one", async (_a, messages) => {
      concurrent++;
      max = Math.max(max, concurrent);
      seen.push(messages.length);
      if (seen.length === 1) await post({ source: "test", kind: "more", mailbox: "one" });
      await new Promise((r) => setTimeout(r, 20));
      concurrent--;
    });
    await post({ source: "test", kind: "first", mailbox: "one" });
    await rt.drain();
    expect(max).toBe(1);
    expect(seen).toEqual([1, 1]);
  });

  it("records a failed turn and moves on instead of retrying forever", async () => {
    let calls = 0;
    const rt = new Runtime();
    rt.register("poison", async () => {
      calls++;
      throw new Error("boom");
    });
    await post({ source: "test", kind: "bad", mailbox: "poison" });
    await rt.drain();
    await rt.drain();
    expect(calls).toBe(1);
    const [error] = await listEvents({ kind: "agent.error" });
    expect(String(error!.payload["error"])).toContain("boom");
  });

  it("runs idle work only when nothing is pending or running", async () => {
    let idleRuns = 0;
    let busy = true;
    const rt = new Runtime();
    rt.registerIdle("chores", async () => idleRuns++, { minIntervalMs: 0, maxIntervalMs: 60_000 });
    rt.register("work", async () => {
      await new Promise((r) => setTimeout(r, 30));
      busy = false;
    });
    await post({ source: "test", kind: "task", mailbox: "work" });
    await rt.step();
    expect(busy).toBe(false);
    expect(idleRuns).toBe(0);
    await rt.step();
    expect(idleRuns).toBe(1);
  });
});

describe("executions", () => {
  it("that a restart lost are reported to their owner, so nothing waits forever", async () => {
    const item = await createWorkItem("interrupted");
    const started = await appendEvent({
      source: "agent:manager",
      kind: "execution.started",
      workItemId: item.id,
      executionId: "ex_lost",
      payload: { instructions: "x", root: "ev_root" },
    });
    await createExecution("ex_lost", item.id, `manager:${item.id}`);
    expect(await recoverExecutions()).toBe(1);
    expect((await getExecution("ex_lost"))?.status).toBe("lost");
    const [finished] = await pendingMessages(`manager:${item.id}`);
    expect(finished!.payload).toMatchObject({ ok: false, lost: true, root: "ev_root" });
    expect(finished!.causedBy).toBe(started.id);
  });
});

describe("work tree", () => {
  it("lists a subtree parents first", async () => {
    const root = await createWorkItem("root");
    const a = await createWorkItem("a", "test", { parentId: root.id });
    const b = await createWorkItem("b", "test", { parentId: a.id });
    expect((await subtree(root.id)).map((i) => i.id)).toEqual([root.id, a.id, b.id]);
  });

  it("declares which facts bubble: questions and misroutes climb, tool traffic does not", () => {
    expect(bubbles("escalation.raised")).toBe(true);
    expect(bubbles("message.reroute_requested")).toBe(true);
    expect(bubbles("side_effect.intent")).toBe(false);
    expect(bubbles("execution.finished")).toBe(false);
  });
});
