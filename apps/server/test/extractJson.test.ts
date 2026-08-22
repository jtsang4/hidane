import { describe, expect, it } from "vitest";
import { extractJson } from "../src/agents/pi.js";

describe("extractJson", () => {
  it("parses a bare JSON object", () => {
    expect(extractJson('{"action":"reply","reply":"hi"}')).toEqual({
      action: "reply",
      reply: "hi",
    });
  });

  it("parses a fenced json block", () => {
    const text = 'Sure!\n```json\n{"action":"new_work_item","title":"T"}\n```\nDone.';
    expect(extractJson(text)).toEqual({ action: "new_work_item", title: "T" });
  });

  it("parses JSON embedded in prose with nested braces and strings", () => {
    const text = 'I decided: {"a":{"b":"with } brace"},"c":2} — that is all.';
    expect(extractJson(text)).toEqual({ a: { b: "with } brace" }, c: 2 });
  });

  it("returns null when no JSON present", () => {
    expect(extractJson("no json here")).toBeNull();
  });
});
