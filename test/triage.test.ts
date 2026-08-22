import { describe, expect, it } from "vitest";
import { triageEvent, needsTriage } from "../src/kernel/triage.js";
import type { HidaneEvent } from "../src/kernel/events.js";

function ev(partial: Partial<HidaneEvent>): HidaneEvent {
  return {
    seq: 1,
    id: "ev_x",
    ts: new Date().toISOString(),
    source: "connector:timer",
    kind: "connector.heartbeat",
    threadId: null,
    workItemId: null,
    executionId: null,
    payload: {},
    ...partial,
  };
}

describe("triage rules", () => {
  it("heartbeats are record-only (no model wake)", () => {
    const res = triageEvent(ev({ kind: "connector.heartbeat" }));
    expect(res.action).toBe("record");
  });

  it("webhooks wake the primary", () => {
    const res = triageEvent(
      ev({ kind: "connector.webhook", source: "connector:webhook:test" }),
    );
    expect(res.action).toBe("wake_primary");
  });

  it("unknown connector events default to record", () => {
    const res = triageEvent(ev({ kind: "connector.unknown" }));
    expect(res.rule).toBe("default-record");
  });

  it("agent and kernel events never re-enter triage", () => {
    expect(needsTriage(ev({ source: "agent:primary", kind: "agent.reply" }))).toBe(false);
    expect(needsTriage(ev({ source: "kernel:triage", kind: "triage.decision" }))).toBe(false);
    expect(needsTriage(ev({ source: "connector:timer", kind: "connector.heartbeat" }))).toBe(true);
  });
});
