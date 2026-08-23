import { describe, expect, it } from "vitest";
import type { HidaneEvent } from "../src/lib/api.js";
import { elapsedSeconds, pendingState, runningWorkItems } from "../src/lib/pending.js";
import {
  livenessFrom,
  shouldReconnect,
  RECONNECT_AFTER_MS,
  STALE_AFTER_MS,
} from "../src/lib/live.js";

let seq = 0;
function ev(partial: Partial<HidaneEvent> & { kind: string }): HidaneEvent {
  seq += 1;
  return {
    seq,
    id: `ev_${seq}`,
    ts: new Date(Date.UTC(2026, 7, 23, 0, 0, seq)).toISOString(),
    source: "test",
    threadId: "main",
    workItemId: null,
    executionId: null,
    payload: {},
    ...partial,
  } as HidaneEvent;
}

describe("pendingState", () => {
  it("is idle when the last message was answered", () => {
    const events = [ev({ kind: "user.message" }), ev({ kind: "agent.reply" })];
    expect(pendingState(events)).toEqual({ active: false, since: null, phase: null });
  });

  it("waits on a user message with no answer after it", () => {
    // seq order matters, not array order: the reply is the EARLIER event here.
    const earlierReply = ev({ kind: "agent.reply" });
    const user = ev({ kind: "user.message" });
    const state = pendingState([earlierReply, user]);
    expect(state.active).toBe(true);
    expect(state.phase).toBe("routing");
    expect(state.since).toBe(user.ts);
  });

  it("an error counts as an answer — the wait must end", () => {
    const events = [ev({ kind: "user.message" }), ev({ kind: "agent.error" })];
    expect(pendingState(events).active).toBe(false);
  });

  it("reports executing while a worker runs, which is the longer wait", () => {
    const started = ev({ kind: "execution.started", executionId: "ex_1" });
    const state = pendingState([ev({ kind: "user.message" }), started]);
    expect(state).toEqual({ active: true, since: started.ts, phase: "executing" });
  });

  it("clears once the execution finishes and the reply lands", () => {
    const events = [
      ev({ kind: "user.message" }),
      ev({ kind: "execution.started", executionId: "ex_1" }),
      ev({ kind: "execution.finished", executionId: "ex_1" }),
      ev({ kind: "agent.reply" }),
    ];
    expect(pendingState(events).active).toBe(false);
  });

  it("is order-independent: out-of-order input must not invent a pending state", () => {
    const user = ev({ kind: "user.message" });
    const reply = ev({ kind: "agent.reply" });
    expect(pendingState([reply, user].sort((a, b) => b.seq - a.seq)).active).toBe(false);
  });
});

describe("runningWorkItems", () => {
  it("lists only items whose execution has not finished", () => {
    const events = [
      ev({ kind: "execution.started", executionId: "ex_a", workItemId: "wi_a" }),
      ev({ kind: "execution.started", executionId: "ex_b", workItemId: "wi_b" }),
      ev({ kind: "execution.finished", executionId: "ex_a", workItemId: "wi_a" }),
    ];
    expect([...runningWorkItems(events)]).toEqual(["wi_b"]);
  });

  it("is empty when nothing is running", () => {
    expect(runningWorkItems([ev({ kind: "user.message" })]).size).toBe(0);
  });
});

describe("elapsedSeconds", () => {
  it("rounds to whole seconds and never goes negative", () => {
    const iso = new Date(Date.UTC(2026, 7, 23, 0, 0, 0)).toISOString();
    expect(elapsedSeconds(iso, Date.UTC(2026, 7, 23, 0, 0, 42))).toBe(42);
    expect(elapsedSeconds(iso, Date.UTC(2026, 7, 22))).toBe(0);
  });
});

describe("livenessFrom", () => {
  it("is connecting while dialling, before anything has arrived", () => {
    expect(livenessFrom(1_000_000, false, 1_000_000 + 3_000)).toBe("connecting");
  });

  it("is live while pings keep arriving", () => {
    expect(livenessFrom(1_000_000, true, 1_000_000 + 20_000)).toBe("live");
  });

  it("goes offline once the stream falls silent", () => {
    // A killed server leaves EventSource OPEN with no error event, so silence
    // is the only signal that the view has stopped being current.
    expect(livenessFrom(1_000_000, true, 1_000_000 + 41_000)).toBe("offline");
  });

  it("a server that never comes back reads offline, not connecting forever", () => {
    // Regression: restarting the clock per reconnect attempt pinned the UI to
    // "connecting" indefinitely, because re-dialling outpaced the stale window.
    expect(livenessFrom(0, false, STALE_AFTER_MS + 1)).toBe("offline");
  });

  it("tolerates two missed pings before giving up", () => {
    expect(livenessFrom(0, true, STALE_AFTER_MS)).toBe("live");
    expect(livenessFrom(0, true, STALE_AFTER_MS + 1)).toBe("offline");
    expect(STALE_AFTER_MS).toBeGreaterThan(2 * 15_000);
  });
});

describe("shouldReconnect", () => {
  it("retries only when the stream is quiet AND the attempt has had a chance", () => {
    const t = RECONNECT_AFTER_MS + 1;
    expect(shouldReconnect(0, 0, t)).toBe(true);
    // A fresh attempt is given time before being torn down again.
    expect(shouldReconnect(0, t, t)).toBe(false);
    // Recent traffic means no reconnect regardless of attempt age.
    expect(shouldReconnect(t, 0, t)).toBe(false);
  });

  it("re-dials before the stream is declared dead, so recovery is quick", () => {
    expect(RECONNECT_AFTER_MS).toBeLessThan(STALE_AFTER_MS);
  });
});
