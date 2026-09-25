import { describe, expect, it } from "vitest";
import type { HidaneEvent } from "../src/lib/api.js";
import { buildTurns, turnRouting } from "../src/lib/conversation.js";

let seq = 0;
function ev(kind: string, payload: Record<string, unknown> = {}, extra: Partial<HidaneEvent> = {}): HidaneEvent {
  seq += 1;
  return {
    seq,
    id: extra.id ?? `ev_${seq}`,
    ts: new Date(2026, 8, 24, 10, 0, seq).toISOString(),
    source: "test",
    kind,
    threadId: "main",
    workItemId: null,
    executionId: null,
    payload,
    ...extra,
  };
}

describe("conversation turns", () => {
  it("files a late answer under the message it answers, not at the bottom", () => {
    const first = ev("user.message", { text: "整理周报" }, { id: "m1" });
    const second = ev("user.message", { text: "明天下午有空吗" }, { id: "m2" });
    const quick = ev("agent.reply", { text: "有空", of: "m2", root: "m2" });
    const late = ev("agent.reply", { text: "周报好了", root: "m1" }, { threadId: "th_x", workItemId: "wi_a" });
    const turns = buildTurns([first, second, quick, late]);
    expect(turns.map((t) => t.root)).toEqual(["m1", "m2"]);
    expect(turns[0]!.answers.map((a) => a.payload["text"])).toEqual(["周报好了"]);
    expect(turns[1]!.answers.map((a) => a.payload["text"])).toEqual(["有空"]);
    expect(turns[0]!.lastSeq).toBe(late.seq);
  });

  it("keeps the newest attribution and records the item a message created", () => {
    const m = ev("user.message", { text: "写爬虫" }, { id: "m1" });
    const created = ev("message.attributed", { of: "m1", by: "model", created: true, title: "爬虫" }, { workItemId: "wi_a" });
    const moved = ev("message.attributed", { of: "m1", by: "user", previous: "wi_a" }, { workItemId: "wi_b" });
    const [turn] = buildTurns([m, created, moved]);
    expect(turn!.createdItem).toBe("wi_a");
    expect(turn!.attribution?.workItemId).toBe("wi_b");
  });

  it("closes a which-one question once the person has picked", () => {
    const m = ev("user.message", { text: "颜色再深一点" }, { id: "m1" });
    const ask = ev("attribution.ambiguous", { of: "m1", candidates: [] });
    expect(buildTurns([m, ask])[0]!.ambiguous).not.toBeNull();
    const picked = ev("message.attributed", { of: "m1", by: "user" }, { workItemId: "wi_a" });
    expect(buildTurns([m, ask, picked])[0]!.ambiguous).toBeNull();
  });

  it("labels answers to things the person did not say", () => {
    const reply = ev("agent.reply", { text: "已处理", root: "ev_hook", rootKind: "external", rootText: "github push" });
    const [turn] = buildTurns([reply]);
    expect(turn!.message).toBeNull();
    expect(turn!.origin).toEqual({ kind: "external", text: "github push" });
  });

  it("reads history written before answers carried a root the old linear way", () => {
    const m = ev("user.message", { text: "旧消息" }, { id: "m_old" });
    const threadCopy = ev("agent.reply", { text: "旧回复" }, { threadId: "th_old", workItemId: "wi_old" });
    const mirror = ev("agent.reply", { text: "旧回复" }, { workItemId: "wi_old" });
    const [turn] = buildTurns([m, threadCopy, mirror]);
    // The old runtime wrote the answer twice; only the main-thread mirror shows.
    expect(turn!.answers.map((a) => a.id)).toEqual([mirror.id]);
  });

  it("shows routing only until the first sign of life", () => {
    const m = ev("user.message", { text: "hi" }, { id: "m1" });
    const soon = Date.parse(m.ts) + 60_000;
    expect(turnRouting(buildTurns([m])[0]!, soon)).toBe(true);
    const attributed = ev("message.attributed", { of: "m1" }, { workItemId: "wi_a" });
    expect(turnRouting(buildTurns([m, attributed])[0]!, soon)).toBe(false);
  });

  it("does not show old history as still routing", () => {
    // Answers written before they named their message cannot be matched to it.
    const m = ev("user.message", { text: "上个月的消息" }, { id: "m1" });
    expect(turnRouting(buildTurns([m])[0]!, Date.parse(m.ts) + 3600_000)).toBe(false);
  });

  it("leaves a subtask's report to its parent out of the conversation", () => {
    const m = ev("user.message", { text: "调研" }, { id: "m1" });
    const child = ev("agent.reply", { text: "requests 笔记", root: "m1", child: true }, { workItemId: "wi_c" });
    const summary = ev("agent.reply", { text: "对比表", root: "m1" }, { workItemId: "wi_p" });
    expect(buildTurns([m, child, summary])[0]!.answers.map((a) => a.payload["text"])).toEqual(["对比表"]);
  });
});
