import { describe, expect, it } from "vitest";
import { createReplyExtractor } from "../src/agents/replyStream.js";

/** Feed a whole payload one character at a time — the worst case a real token
 *  stream can produce, and the one that exposes every partial-state bug. */
function drip(payload: string): string {
  const extract = createReplyExtractor();
  let out = "";
  for (const ch of payload) out += extract(ch);
  return out;
}

describe("createReplyExtractor", () => {
  it("yields the reply and nothing else", () => {
    expect(drip('{"action":"reply","reply":"hello there"}')).toBe("hello there");
  });

  it("is not fooled by the action value that precedes the key", () => {
    // `"reply"` appears first as the value of `action`. Keying off the colon is
    // what keeps `reply` from being read as the empty string between `","`.
    const extract = createReplyExtractor();
    expect(extract('{"action":"reply","reply":"ok"}')).toBe("ok");
  });

  it("emits each chunk exactly once", () => {
    const extract = createReplyExtractor();
    expect(extract('{"action":"reply","reply":"ab')).toBe("ab");
    expect(extract("cd")).toBe("cd");
    expect(extract('"}')).toBe("");
  });

  it("decodes escapes", () => {
    expect(drip('{"reply":"a\\nb\\t\\"c\\"\\\\d"}')).toBe('a\nb\t"c"\\d');
  });

  it("holds back an escape that is still arriving", () => {
    const extract = createReplyExtractor();
    // A lone trailing backslash decoded now would emit a character the model
    // never wrote, and the real one could never be corrected afterwards.
    expect(extract('{"reply":"x\\')).toBe("x");
    expect(extract("nY")).toBe("\nY");
  });

  it("holds back a partial unicode escape", () => {
    const extract = createReplyExtractor();
    expect(extract('{"reply":"\\u4f')).toBe("");
    expect(extract("60"), "completes to 你").toBe("你");
  });

  it("never emits half a surrogate pair", () => {
    const extract = createReplyExtractor();
    expect(extract('{"reply":"\\ud83d')).toBe("");
    expect(extract('\\ude00"}')).toBe("😀");
  });

  it("stops at the closing quote", () => {
    expect(drip('{"reply":"done","action":"reply"}')).toBe("done");
  });

  it("yields nothing for decisions that carry no reply", () => {
    // The user-visible text for these is composed by the runtime afterwards;
    // streaming the model's raw fields would leak routing internals.
    expect(drip('{"action":"new_work_item","title":"T","brief":"B"}')).toBe("");
    expect(drip('{"action":"route_to_work_item","work_item_id":"wi_1","message":"m"}')).toBe("");
  });

  it("handles CJK, which arrives mid-token far more often than ASCII", () => {
    expect(drip('{"action":"reply","reply":"你好，世界。"}')).toBe("你好，世界。");
  });

  it("survives text that itself mentions the reply key", () => {
    expect(drip('{"reply":"use \\"reply\\": here"}')).toBe('use "reply": here');
  });
});
