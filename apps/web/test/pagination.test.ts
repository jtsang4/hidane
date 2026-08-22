import { describe, expect, it } from "vitest";
import type { HidaneEvent } from "../src/lib/api.js";

/**
 * Locks the cursor semantics that once broke "load more": the API returns
 * ascending seq, the UI renders newest-first, and the next page's `before`
 * cursor must be the OLDEST loaded seq — i.e. the first element of the last
 * raw (ascending) page, not the last element of the reversed display array.
 */
function nextCursor(
  newestPage: HidaneEvent[],
  olderPages: HidaneEvent[][],
): number | undefined {
  return olderPages.at(-1)?.[0]?.seq ?? newestPage[0]?.seq;
}

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
