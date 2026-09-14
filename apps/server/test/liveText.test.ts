import { describe, expect, it } from "vitest";
import {
  beginLiveText,
  liveTextSnapshot,
  subscribeLiveText,
  type LiveTextFrame,
} from "../src/agents/liveText.js";

function collect(): { frames: LiveTextFrame[]; stop: () => void } {
  const frames: LiveTextFrame[] = [];
  const stop = subscribeLiveText((frame) => frames.push(frame));
  return { frames, stop };
}

describe("liveText", () => {
  it("emits one delta frame per push and closes with done", () => {
    const { frames, stop } = collect();
    const live = beginLiveText("main");
    live.push("你");
    live.push("好");
    live.end();
    stop();
    expect(frames.map((f) => f.delta ?? (f.done ? "<done>" : "?"))).toEqual(["你", "好", "<done>"]);
    expect(new Set(frames.map((f) => f.id))).toEqual(new Set([live.id]));
    expect(frames.every((f) => f.threadId === "main")).toBe(true);
  });

  it("offers an open reply as an absolute snapshot, then drops it when it ends", () => {
    const live = beginLiveText("th_1");
    live.push("abc");
    const mid = liveTextSnapshot().filter((f) => f.id === live.id);
    expect(mid).toEqual([{ id: live.id, threadId: "th_1", text: "abc" }]);
    live.end();
    expect(liveTextSnapshot().some((f) => f.id === live.id)).toBe(false);
  });

  it("keeps a reply that has produced nothing out of the snapshot", () => {
    // A decision carrying no `reply` pushes no text. Replaying an empty bubble
    // to a joining client would hide the pending indicator behind nothing.
    const live = beginLiveText("th_2");
    expect(liveTextSnapshot().some((f) => f.id === live.id)).toBe(false);
    live.end();
  });

  it("ignores empty deltas so a no-reply decision stays silent", () => {
    const { frames, stop } = collect();
    const live = beginLiveText("th_3");
    live.push("");
    stop();
    live.end();
    expect(frames).toEqual([]);
  });

  it("goes quiet after end, including a late push", () => {
    const { frames, stop } = collect();
    const live = beginLiveText("th_4");
    live.end();
    live.end();
    live.push("late");
    stop();
    expect(frames).toEqual([{ id: live.id, threadId: "th_4", done: true }]);
  });

  it("stops delivering once unsubscribed", () => {
    // Every SSE client adds a listener; one that outlived its connection would
    // leak for the lifetime of the daemon.
    const { frames, stop } = collect();
    const live = beginLiveText("th_5");
    live.push("seen");
    stop();
    live.push("unseen");
    live.end();
    expect(frames.map((f) => f.delta)).toEqual(["seen"]);
  });

  it("caps the replay snapshot while deltas keep flowing", () => {
    const { frames, stop } = collect();
    const live = beginLiveText("th_6");
    const chunk = "x".repeat(4_000);
    for (let i = 0; i < 10; i++) live.push(chunk);
    const snapshot = liveTextSnapshot().find((f) => f.id === live.id);
    stop();
    live.end();
    expect(frames).toHaveLength(10);
    expect(snapshot?.text.length).toBeLessThanOrEqual(36_000);
    expect(snapshot?.text.length).toBeGreaterThanOrEqual(32_000);
  });

  it("keeps concurrent replies on separate ids and threads", () => {
    const { frames, stop } = collect();
    const a = beginLiveText("main");
    const b = beginLiveText("th_7");
    a.push("A");
    b.push("B");
    a.end();
    b.end();
    stop();
    expect(frames.filter((f) => f.id === a.id).map((f) => f.threadId)).toEqual(["main", "main"]);
    expect(frames.filter((f) => f.id === b.id).map((f) => f.threadId)).toEqual(["th_7", "th_7"]);
    expect(a.id).not.toBe(b.id);
  });
});
