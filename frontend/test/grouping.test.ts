import { describe, expect, it } from "vitest";
import { conversationEvents, executionGroups, payloadText } from "../src/lib/grouping.js";
import type { HidaneEvent } from "../src/lib/api.js";

let seq = 0;
function ev(partial: Partial<HidaneEvent>): HidaneEvent {
  seq++;
  return {
    seq,
    id: `ev_${seq}`,
    ts: new Date().toISOString(),
    source: "test",
    kind: "user.message",
    threadId: "main",
    workItemId: null,
    executionId: null,
    payload: {},
    ...partial,
  };
}

describe("grouping", () => {
  it("filters conversation kinds only", () => {
    const events = [
      ev({ kind: "user.message", payload: { text: "hi" } }),
      ev({ kind: "route.decision" }),
      ev({ kind: "agent.reply", payload: { text: "hello" } }),
      ev({ kind: "connector.heartbeat" }),
    ];
    expect(conversationEvents(events).map((e) => e.kind)).toEqual([
      "user.message",
      "agent.reply",
    ]);
  });

  it("groups execution lifecycle by executionId with ok verdict", () => {
    const events = [
      ev({ kind: "execution.started", executionId: "ex_1", payload: { instructions: "do" } }),
      ev({ kind: "side_effect.intent", executionId: "ex_1", payload: { tool: "write" } }),
      ev({ kind: "side_effect.result", executionId: "ex_1", payload: { tool: "write", isError: false } }),
      ev({ kind: "execution.finished", executionId: "ex_1", payload: { ok: true } }),
      ev({ kind: "execution.started", executionId: "ex_2", payload: {} }),
    ];
    const groups = executionGroups(events);
    expect(groups).toHaveLength(2);
    expect(groups[0]!.executionId).toBe("ex_1");
    expect(groups[0]!.ok).toBe(true);
    expect(groups[0]!.sideEffects).toHaveLength(2);
    expect(groups[1]!.ok).toBeNull();
  });

  it("payloadText prefers text then note then error, falls back to json", () => {
    expect(payloadText(ev({ payload: { text: "t" } }))).toBe("t");
    expect(payloadText(ev({ payload: { note: "n" } }))).toBe("n");
    expect(payloadText(ev({ payload: { error: "e" } }))).toBe("e");
    expect(payloadText(ev({ payload: { x: 1 } }))).toBe('{"x":1}');
  });
});
