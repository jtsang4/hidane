import { beforeEach, describe, expect, it } from "vitest";
import {
  applyLiveFrame,
  liveRepliesFor,
  maxSeq,
  noteLiveEvent,
  resetLiveText,
} from "../src/lib/liveText.js";

function event(seq: number, kind: string, threadId: string | null = "main") {
  return { seq, kind, threadId };
}

/** The reader's screen before the refetch that the announcement triggers. */
const NOT_YET_RENDERED = 0;

describe("liveText", () => {
  beforeEach(() => resetLiveText());

  it("accumulates deltas into one bubble", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "你" }));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "好" }));
    expect(liveRepliesFor("main", NOT_YET_RENDERED)[0]?.text).toBe("你好");
  });

  it("keeps threads apart", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "a" }));
    applyLiveFrame(JSON.stringify({ id: "ls_2", threadId: "th_x", delta: "b" }));
    expect(liveRepliesFor("main", NOT_YET_RENDERED).map((r) => r.text)).toEqual(["a"]);
    expect(liveRepliesFor("th_x", NOT_YET_RENDERED).map((r) => r.text)).toEqual(["b"]);
  });

  it("replaces rather than appends when replaying a snapshot", () => {
    // A client connecting mid-reply is sent the whole text so far. Appending it
    // to what arrived after would duplicate the overlap.
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", text: "abc" }));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", text: "abcd" }));
    expect(liveRepliesFor("main", NOT_YET_RENDERED)[0]?.text).toBe("abcd");
  });

  it("marks done without dropping the text", () => {
    // The durable event is still a poll away; clearing here would blank the
    // reply and then print it again.
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", done: true }));
    const reply = liveRepliesFor("main", NOT_YET_RENDERED)[0];
    expect(reply?.text).toBe("hi");
    expect(reply?.done).toBe(true);
  });

  it("holds the bubble between the announcement and the render that replaces it", () => {
    // The flicker this guards against: retiring on the announcement alone left
    // the finished reply with no bubble at all for a round trip, so it vanished
    // and the pending spinner came back before the durable copy appeared.
    noteLiveEvent(event(10, "user.message"));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(11, "agent.reply"));
    expect(liveRepliesFor("main", maxSeq([event(10, "user.message")]))).toHaveLength(1);
  });

  it("retires the bubble once the durable reply is on screen", () => {
    noteLiveEvent(event(10, "user.message"));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(11, "agent.reply"));
    const rendered = maxSeq([event(10, "user.message"), event(11, "agent.reply")]);
    expect(liveRepliesFor("main", rendered)).toEqual([]);
  });

  it("needs the announcement, not just a newer render, to retire a bubble", () => {
    // Rendering alone must not retire: on a fresh load the history already
    // holds answers newer than any bubble about to open, and treating those as
    // replacements would retire every reply the instant it started. The cost is
    // that a `hidane` frame missed across a reconnect leaves the bubble up
    // until the next reply begins — pre-existing, and the safer direction.
    noteLiveEvent(event(10, "user.message"));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    applyLiveFrame(JSON.stringify({ id: "ls_2", threadId: "main", delta: "next" }));
    expect(liveRepliesFor("main", maxSeq([event(99, "agent.reply")]))).toHaveLength(2);
  });

  it("ignores an older reply that was already on screen", () => {
    // Backfill and out-of-order delivery must not blank a bubble that is still
    // being written; only an answer newer than the bubble supersedes it.
    noteLiveEvent(event(10, "user.message"));
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(9, "agent.reply"));
    expect(liveRepliesFor("main", maxSeq([event(10, "user.message")]))).toHaveLength(1);
  });

  it("does not retire on an answer to a different thread", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(99, "agent.reply", "th_other"));
    expect(liveRepliesFor("main", 99)).toHaveLength(1);
  });

  it("retires on an error too, or the bubble would never clear", () => {
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "hi" }));
    noteLiveEvent(event(5, "agent.error"));
    expect(liveRepliesFor("main", 5)).toEqual([]);
  });

  it("forgets a retired bubble once the next reply starts", () => {
    // Nothing deletes a held bubble at render time, so the next one on the
    // thread has to, or every finished reply would be kept for the page's life.
    applyLiveFrame(JSON.stringify({ id: "ls_1", threadId: "main", delta: "first" }));
    noteLiveEvent(event(11, "agent.reply"));
    applyLiveFrame(JSON.stringify({ id: "ls_2", threadId: "main", delta: "second" }));
    expect(liveRepliesFor("main", 11).map((r) => r.text)).toEqual(["second"]);
    // ls_1 is gone outright, not merely filtered out of the current view.
    expect(liveRepliesFor("main", NOT_YET_RENDERED).map((r) => r.text)).toEqual(["second"]);
  });

  it("drops malformed frames instead of throwing", () => {
    // These arrive on the same connection as real events; a parse error that
    // escaped would take the live lane down with it.
    expect(() => applyLiveFrame("not json")).not.toThrow();
    expect(() => applyLiveFrame(JSON.stringify({ threadId: "main", delta: "x" }))).not.toThrow();
    expect(() => applyLiveFrame(JSON.stringify({ id: "ls_1", delta: "x" }))).not.toThrow();
    expect(liveRepliesFor("main", NOT_YET_RENDERED)).toEqual([]);
  });

  it("reports the highest seq regardless of order", () => {
    expect(maxSeq([])).toBe(0);
    expect(maxSeq([event(7, "agent.reply"), event(3, "user.message")])).toBe(7);
  });
});
