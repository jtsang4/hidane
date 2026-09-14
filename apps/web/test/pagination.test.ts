import { describe, expect, it } from "vitest";
import type { HidaneEvent } from "../src/lib/api.js";
import { mergeById, nextCursor } from "../src/lib/pagination.js";

/**
 * Locks the cursor semantics that once broke "load more": the API returns
 * ascending seq, the UI renders newest-first, and the next page's `before`
 * cursor must be the OLDEST loaded seq — i.e. the first element of the last
 * raw (ascending) page, not the last element of the reversed display array.
 */

function ev(seq: number): HidaneEvent {
  return {
    seq,
    id: `ev_${seq}`,
    ts: new Date().toISOString(),
    source: "s",
    kind: "k",
    threadId: null,
    workItemId: null,
    executionId: null,
    payload: {},
  };
}

describe("events pagination cursor", () => {
  it("uses the oldest seq of the newest page as the first cursor", () => {
    const newestPage = [ev(10), ev(11), ev(12)]; // ascending from the API
    expect(nextCursor(newestPage, [])).toBe(10);
  });

  it("advances backwards using each newly loaded older page", () => {
    const newestPage = [ev(10), ev(11), ev(12)];
    const older1 = [ev(7), ev(8), ev(9)];
    expect(nextCursor(newestPage, [older1])).toBe(7);
    const older2 = [ev(4), ev(5), ev(6)];
    expect(nextCursor(newestPage, [older1, older2])).toBe(4);
  });

  it("never returns a newer cursor than already displayed (the old bug)", () => {
    const newestPage = [ev(10), ev(11), ev(12)];
    const older1 = [ev(7), ev(8), ev(9)];
    const cursor = nextCursor(newestPage, [older1]) as number;
    const displayedOldest = Math.min(
      ...[...newestPage, ...older1].map((e) => e.seq),
    );
    expect(cursor).toBeLessThanOrEqual(displayedOldest);
  });

  it("is undefined when nothing is loaded", () => {
    expect(nextCursor([], [])).toBeUndefined();
  });
});

/**
 * The union exists because the newest page slides forward on every live event
 * while older pages keep the exclusive `before` they were fetched with.
 */
describe("mergeById", () => {
  const at = (seq: number, id = `ev_${seq}`) => ({ ...ev(seq), id });

  it("orders by seq across pages regardless of argument order", () => {
    const merged = mergeById([at(5), at(6)], [at(1), at(2)]);
    expect(merged.map((e) => e.seq)).toEqual([1, 2, 5, 6]);
  });

  it("de-duplicates overlapping pages by id", () => {
    const merged = mergeById([at(1), at(2)], [at(2), at(3)]);
    expect(merged.map((e) => e.seq)).toEqual([1, 2, 3]);
  });

  it("retains an event the newest page no longer returns", () => {
    // The window slid past it. The log is append-only, so it was not deleted.
    const firstHead = [at(10), at(11)];
    const slidHead = [at(12), at(13)];
    expect(mergeById(firstHead, slidHead).map((e) => e.seq)).toEqual([10, 11, 12, 13]);
  });

  it("closes the gap that plain concatenation opens", () => {
    // Older page was cut at `before: 10`; the head has since moved to 12+.
    const older = [at(8), at(9)];
    const head = [at(12), at(13)];
    const strayed = [at(10), at(11)]; // observed by an earlier head fetch
    expect(mergeById(older, strayed, head).map((e) => e.seq)).toEqual([8, 9, 10, 11, 12, 13]);
    // Concatenation keeps only what the two surviving pages hold.
    expect([...older, ...head].map((e) => e.seq)).toEqual([8, 9, 12, 13]);
  });
});
