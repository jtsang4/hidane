import { describe, expect, it } from "vitest";
import { isPinnedToBottom, PINNED_SLACK_PX } from "../src/lib/scroll.js";

/**
 * Guards the autoscroll decision that produced the visible "loads from the
 * first message, then scrolls all the way down" behaviour: the chat used to
 * smooth-scroll to the bottom on every update regardless of where the reader
 * was. The predicate below is what replaces that unconditional scroll.
 */
describe("isPinnedToBottom", () => {
  it("treats an unfilled viewport as pinned, so a fresh load starts at the newest message", () => {
    // Nothing rendered yet: scrollHeight collapses to the viewport height.
    expect(isPinnedToBottom({ offsetHeight: 600, scrollTop: 0, scrollHeight: 600 })).toBe(true);
  });

  it("is pinned when scrolled to the exact bottom of a long history", () => {
    expect(isPinnedToBottom({ offsetHeight: 600, scrollTop: 9400, scrollHeight: 10000 })).toBe(true);
  });

  it("is not pinned when the reader has scrolled up into history", () => {
    expect(isPinnedToBottom({ offsetHeight: 600, scrollTop: 0, scrollHeight: 10000 })).toBe(false);
  });

  it("tolerates a small gap above the bottom", () => {
    const nearly = { offsetHeight: 600, scrollTop: 9400 - (PINNED_SLACK_PX - 1), scrollHeight: 10000 };
    expect(isPinnedToBottom(nearly)).toBe(true);
  });

  it("stops being pinned once the gap exceeds the slack", () => {
    const away = { offsetHeight: 600, scrollTop: 9400 - PINNED_SLACK_PX, scrollHeight: 10000 };
    expect(isPinnedToBottom(away)).toBe(false);
  });
});
