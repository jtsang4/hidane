import { describe, expect, it } from "vitest";
import { cutRange, matchCommands, mentionAt, parseSlash, rankCandidates, slashAt, type Candidate } from "../src/lib/mentions.js";

describe("mentions", () => {
  it("finds the @ word the caret is in", () => {
    expect(mentionAt("@", 1)).toEqual({ start: 0, end: 1, query: "" });
    expect(mentionAt("hi @rs", 6)).toEqual({ start: 3, end: 6, query: "rs" });
    expect(mentionAt("中文 @订机票", 7)).toEqual({ start: 3, end: 7, query: "订机票" });
    expect(mentionAt("mail me@example", 15)).toBeNull();
    expect(mentionAt("@rss done", 9)).toBeNull();
  });

  it("cuts the picked word without leaving a double space", () => {
    expect(cutRange("@rs 改一下", 0, 3)).toEqual({ text: "改一下", caret: 0 });
    expect(cutRange("请 @rs 改一下", 2, 5)).toEqual({ text: "请 改一下", caret: 2 });
    expect(cutRange("请@x", 1, 3)).toEqual({ text: "请", caret: 1 });
  });

  it("ranks what waits on the person first and hides archived tasks until searched for", () => {
    const c = (id: string, title: string, status: Candidate["status"], state: Candidate["state"], updatedAt: string): Candidate => ({ id, title, status, state, updatedAt });
    const all = [
      c("wi_a", "Write docs", "open", "idle", "2026-01-03"),
      c("wi_b", "Flights", "open", "waiting", "2026-01-01"),
      c("wi_c", "RSS feed", "open", "review", "2026-01-02"),
      c("wi_d", "Upgrade Go", "open", "running", "2026-01-01"),
      c("wi_e", "Old docs", "closed", null, "2026-01-09"),
      c("wi_f", "Done docs", "done", "done", "2026-01-08"),
    ];
    expect(rankCandidates(all, "", new Set()).map((x) => x.id)).toEqual(["wi_b", "wi_c", "wi_d", "wi_a", "wi_f"]);
    expect(rankCandidates(all, "docs", new Set()).map((x) => x.id)).toEqual(["wi_a", "wi_f", "wi_e"]);
    expect(rankCandidates(all, "", new Set(["wi_b"]), 2).map((x) => x.id)).toEqual(["wi_c", "wi_d"]);
    expect(rankCandidates(all, "wi_e", new Set()).map((x) => x.id)).toEqual(["wi_e"]);
  });
});

describe("slash commands", () => {
  it("offer commands only while the whole message is the command", () => {
    expect(slashAt("/", 1)).toEqual({ start: 0, end: 1, query: "" });
    expect(slashAt("/do", 3)?.query).toBe("do");
    expect(slashAt("/done x", 5)).toBeNull();
    expect(slashAt("a /done", 7)).toBeNull();
    expect(matchCommands("st")).toEqual(["stop"]);
    expect(matchCommands("")).toEqual(["stop", "done", "archive", "reopen"]);
  });

  it("run only an exact command; a path or a sentence is said", () => {
    expect(parseSlash(" /done ")).toBe("done");
    expect(parseSlash("/STOP")).toBe("stop");
    expect(parseSlash("/etc/hosts 里写了什么")).toBeNull();
    expect(parseSlash("/donex")).toBeNull();
    expect(parseSlash("done")).toBeNull();
  });
});
