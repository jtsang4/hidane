import { describe, expect, it } from "vitest";
import { resumeCursor } from "../src/api/routes.js";

const TAIL = Number.MAX_SAFE_INTEGER;

describe("resumeCursor", () => {
  it("starts at the tail when neither side says otherwise", () => {
    expect(resumeCursor(undefined, undefined)).toBe(TAIL);
  });

  it("resumes from Last-Event-ID so a reconnect loses nothing", () => {
    // The gap this closes: events appended while the connection was down were
    // never replayed, and the client had no way to notice they were missing.
    expect(resumeCursor(undefined, "41")).toBe(41);
  });

  it("lets an explicit after override the browser's resume point", () => {
    expect(resumeCursor("10", "41")).toBe(10);
  });

  it("treats an empty Last-Event-ID as absent rather than as seq 0", () => {
    // Number("") is 0, which would replay the entire log to a reconnecting tab.
    expect(resumeCursor(undefined, "")).toBe(TAIL);
    expect(resumeCursor(undefined, "   ")).toBe(TAIL);
  });

  it("refuses non-integer and negative cursors", () => {
    for (const bad of ["abc", "1.5", "-1", "NaN", "1e999"]) {
      expect(resumeCursor(undefined, bad)).toBe(TAIL);
    }
  });

  it("does not fall through to the header when after is malformed", () => {
    // Answering a bad `after` with the browser's resume point would silently
    // answer a different question than the caller asked.
    expect(resumeCursor("abc", "41")).toBe(TAIL);
  });

  it("accepts seq 0, the only way to ask for the whole log", () => {
    expect(resumeCursor("0", undefined)).toBe(0);
  });
});
