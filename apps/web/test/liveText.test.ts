import { beforeEach, describe, expect, it } from "vitest";
import {
  applyLiveFrame,
  liveRepliesFor,
  noteLiveEvent,
  resetLiveText,
} from "../src/lib/liveText.js";

function event(seq: number, kind: string, threadId: string | null = "main") {
  return { seq, kind, threadId };
}

describe("liveText", () => {
  beforeEach(() => resetLiveText());

  it("accumulates deltas into one bubble", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "你" }));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "好" }));
    expect(liveRepliesFor("main")[0]?.text).toBe("你好");
  });

  it("keeps threads apart", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "a" }));
    applyLiveFrame(JSON.stringify({ id: "ls_2", threadId: "th_x", delta: "b" }));
    expect(liveRepliesFor("main").map((r) => r.text)).toEqual(["a"]);
    expect(liveRepliesFor("th_x").map((r) => r.text)).toEqual(["b"]);
  });

  it("replaces rather than appends when replaying a snapshot", () => {
    // A client connecting mid-reply is sent the whole text so far. Appending it
    // to what arrived after would duplicate the overlap.
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", text: "abc" }));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", text: "abcd" }));
    expect(liveRepliesFor("main")[0]?.text).toBe("abcd");
  });

  it("marks done without dropping the text", () => {
    // The durable event is still a poll away; clearing here would blank the
    // reply and then print it again.
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", done: true }));
    const reply = liveRepliesFor("main")[0];
    expect(reply?.text).toBe("hi");
    expect(reply?.done).toBe(true);
  });

  it("retires the bubble when the durable reply lands", () => {
    noteLiveEvent(event(10, "user.message"));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(11, "agent.reply"));
    expect(liveRepliesFor("main")).toEqual([]);
  });

  it("ignores an older reply that was already on screen", () => {
    // Backfill and out-of-order delivery must not blank a bubble that is still
    // being written; only an answer newer than the bubble supersedes it.
    noteLiveEvent(event(10, "user.message"));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(9, "agent.reply"));
    expect(liveRepliesFor("main")).toHaveLength(1);
  });

  it("does not retire on an answer to a different thread", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(99, "agent.reply", "th_other"));
    expect(liveRepliesFor("main")).toHaveLength(1);
  });

  it("retires on an error too, or the bubble would never clear", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(5, "agent.error"));
    expect(liveRepliesFor("main")).toEqual([]);
  });

  it("drops malformed frames instead of throwing", () => {
    // These arrive on the same connection as real events; a parse error that
    // escaped would take the live lane down with it.
    expect(() => applyLiveFrame("not json")).not.toThrow();
    expect(() => applyLiveFrame(JSON.stringify({ threadId: "main", delta: "x" }))).not.toThrow();
    expect(() => applyLiveFrame(JSON.stringify({ id: "ls_1", delta: "x" }))).not.toThrow();
    expect(liveRepliesFor("main")).toEqual([]);
  });
});
