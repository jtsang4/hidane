import { describe, expect, it } from "vitest";
import {
  appendEvent,
  listEvents,
  getCursor,
  commitCursor,
  resetCursor,
  nextBatch,
} from "../src/kernel/events.js";

describe("event log", () => {
  it("appends events with envelope fields and monotonic seq", async () => {
    const a = await appendEvent({
      source: "connector:test",
      kind: "user.message",
      threadId: "main",
      payload: { text: "hello" },
    });
    const b = await appendEvent({
      source: "connector:test",
      kind: "user.message",
      threadId: "main",
      payload: { text: "world" },
    });
    expect(a.id).toMatch(/^ev_/);
    expect(b.seq).toBeGreaterThan(a.seq);
    expect(a.threadId).toBe("main");
    expect(a.payload["text"]).toBe("hello");
  });

  it("filters by thread, kind and day", async () => {
    await appendEvent({ source: "s", kind: "k1", threadId: "main", payload: {} });
    await appendEvent({ source: "s", kind: "k2", threadId: "t2", payload: {} });
    const main = await listEvents({ threadId: "main" });
    expect(main).toHaveLength(1);
    const k2 = await listEvents({ kind: "k2" });
    expect(k2).toHaveLength(1);
    const today = new Date();
    const pad = (n: number) => String(n).padStart(2, "0");
    const day = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`;
    const todays = await listEvents({ day });
    expect(todays).toHaveLength(2);
  });

  it("tail returns last n in ascending order", async () => {
    for (let i = 0; i < 5; i++) {
      await appendEvent({ source: "s", kind: "k", payload: { i } });
    }
    const tail = await listEvents({ tail: 2 });
    expect(tail).toHaveLength(2);
    expect(Number(tail[0]!.payload["i"])).toBe(3);
    expect(Number(tail[1]!.payload["i"])).toBe(4);
  });

  it("consumer cursors: nextBatch, commit, replay via reset", async () => {
    await appendEvent({ source: "s", kind: "a", payload: {} });
    await appendEvent({ source: "s", kind: "b", payload: {} });
    expect(await getCursor("c1")).toBe(0);

    const batch1 = await nextBatch("c1");
    expect(batch1).toHaveLength(2);
    await commitCursor("c1", batch1[1]!.seq);

    const batch2 = await nextBatch("c1");
    expect(batch2).toHaveLength(0);

    // events are never consumed destructively: replay by resetting the cursor
    await resetCursor("c1");
    const replayed = await nextBatch("c1");
    expect(replayed).toHaveLength(2);
    expect(replayed.map((e) => e.kind)).toEqual(["a", "b"]);
  });
});
