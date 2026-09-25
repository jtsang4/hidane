import { afterEach, describe, expect, it, vi } from "vitest";
import { api, type HidaneEvent } from "../src/lib/api.js";
import { buildTurns } from "../src/lib/conversation.js";
import {
  awayFromLatest,
  contextBoundary,
  dayBreaks,
  daysByMonth,
  excerpt,
  highlight,
  loadedRange,
  rootOfEvent,
  searchTerms,
} from "../src/lib/history.js";
import { invalidationFor } from "../src/lib/live.js";
import { atFrom, conversationHref, focusHref } from "../src/lib/router.svelte.js";

let seq = 0;
function ev(kind: string, payload: Record<string, unknown> = {}, extra: Partial<HidaneEvent> = {}): HidaneEvent {
  seq += 1;
  return {
    seq,
    id: extra.id ?? `ev_${seq}`,
    ts: extra.ts ?? new Date(2026, 8, 24, 10, 0, seq).toISOString(),
    source: "test",
    kind,
    threadId: "main",
    workItemId: null,
    executionId: null,
    payload,
    ...extra,
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("history links", () => {
  it("round-trips an event and a focused item through the URL", () => {
    expect(conversationHref({ at: "ev_1" })).toBe("/?at=ev_1");
    expect(conversationHref({ at: "ev_1", focus: "wi_a" })).toBe("/?focus=wi_a&at=ev_1");
    expect(conversationHref({})).toBe("/");
    expect(focusHref("wi_a")).toBe("/?focus=wi_a");
    expect(atFrom("?focus=wi_a&at=ev_1")).toBe("ev_1");
    expect(atFrom("?at=")).toBeNull();
  });

  it("finds the turn a search hit belongs to", () => {
    expect(rootOfEvent(ev("user.message", { text: "hi" }, { id: "m1" }))).toBe("m1");
    expect(rootOfEvent(ev("agent.reply", { text: "ok", root: "m1" }, { threadId: "th_x" }))).toBe("m1");
    expect(rootOfEvent(ev("escalation", { question: "?", of: "m2" }))).toBe("m2");
  });
});

describe("search presentation", () => {
  it("splits on whitespace like the server", () => {
    expect(searchTerms("  部署   staging ")).toEqual(["部署", "staging"]);
  });

  it("marks every occurrence of every term, case-insensitively", () => {
    expect(highlight("Deploy staging, then deploy prod", ["deploy"])).toEqual([
      { text: "Deploy", hit: true },
      { text: " staging, then ", hit: false },
      { text: "deploy", hit: true },
      { text: " prod", hit: false },
    ]);
    expect(highlight("部署方案", ["部署", "方案"])).toEqual([{ text: "部署方案", hit: true }]);
    expect(highlight("", ["x"])).toEqual([]);
  });

  it("excerpts around the first match so it is visible in a long reply", () => {
    const long = `${"前".repeat(200)}关键词${"后".repeat(200)}`;
    const cut = excerpt(long, ["关键词"], 10);
    expect(cut).toBe(`…${"前".repeat(10)}关键词${"后".repeat(7)}…`);
    expect(excerpt("short text", ["text"])).toBe("short text");
  });
});

describe("reading history by day", () => {
  it("opens each day, including the first loaded one", () => {
    const a = ev("user.message", { text: "a" }, { id: "a", ts: "2026-09-24T02:00:00Z" });
    const b = ev("user.message", { text: "b" }, { id: "b", ts: "2026-09-24T09:00:00Z" });
    const c = ev("user.message", { text: "c" }, { id: "c", ts: "2026-09-25T02:00:00Z" });
    const breaks = dayBreaks(buildTurns([a, b, c]), "UTC");
    expect([...breaks]).toEqual([
      ["a", "2026-09-24"],
      ["c", "2026-09-25"],
    ]);
  });

  it("groups days under months without reordering them", () => {
    const groups = daysByMonth([
      { day: "2026-09-02", count: 1, firstId: "x" },
      { day: "2026-09-01", count: 2, firstId: "y" },
      { day: "2026-08-31", count: 3, firstId: "z" },
    ]);
    expect(groups.map((g) => [g.month, g.days.map((d) => d.day)])).toEqual([
      ["2026-09", ["2026-09-02", "2026-09-01"]],
      ["2026-08", ["2026-08-31"]],
    ]);
  });

  it("knows the loaded range for paging both ways", () => {
    expect(loadedRange([{ seq: 7 }, { seq: 3 }, { seq: 9 }])).toEqual({ oldest: 3, newest: 9 });
    expect(loadedRange([])).toBeNull();
  });

  it("marks where the assistant's view begins only when something older is above it", () => {
    const turns = buildTurns([
      ev("user.message", { text: "old" }, { id: "m1" }),
      ev("user.message", { text: "new" }, { id: "m2" }),
    ]);
    expect(contextBoundary(turns, "m2")).toBe("m2");
    expect(contextBoundary(turns, "m1")).toBeNull();
    expect(contextBoundary(turns, "m1", true)).toBe("m1");
    expect(contextBoundary(turns, null)).toBeNull();
    expect(contextBoundary(turns, "unloaded")).toBeNull();
  });
});

describe("the way back to the newest message", () => {
  const base = { mode: "live" as const, follow: true, searching: false, primed: true, hasTurns: true };

  it("appears when scrolled up in the live view or reading older history", () => {
    expect(awayFromLatest(base)).toBe(false);
    expect(awayFromLatest({ ...base, follow: false })).toBe(true);
    // The bottom of a window of history is still not "now".
    expect(awayFromLatest({ ...base, mode: "window", follow: true })).toBe(true);
  });

  it("stays out of the way while searching or before the view has settled", () => {
    expect(awayFromLatest({ ...base, follow: false, searching: true })).toBe(false);
    expect(awayFromLatest({ ...base, follow: false, primed: false })).toBe(false);
    expect(awayFromLatest({ ...base, follow: false, hasTurns: false })).toBe(false);
  });
});

describe("hidden messages", () => {
  it("hides a message already on screen as soon as the hiding arrives", () => {
    const m = ev("user.message", { text: "密钥 abc" }, { id: "m1" });
    const [before] = buildTurns([m]);
    expect(before!.redacted).toBe(false);
    const [after] = buildTurns([m, ev("message.redacted", { of: "m1", root: "m1" })]);
    expect(after!.redacted).toBe(true);
    expect(after!.answers).toHaveLength(0);
  });

  it("trusts the server's mask on copies fetched later", () => {
    const [turn] = buildTurns([ev("user.message", { text: "", redacted: true }, { id: "m1" })]);
    expect(turn!.redacted).toBe(true);
  });

  it("refreshes the conversation when a hiding is recorded", () => {
    expect(invalidationFor({ kind: "message.redacted", threadId: "main", workItemId: null }).now).toContainEqual([
      "conversation",
    ]);
  });
});

describe("history api", () => {
  it("asks for windows, forward pages and filtered pages", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ events: [], hasMore: false, oldestSeq: null })));
    vi.stubGlobal("fetch", fetchMock);
    await api.eventsPage({ conversation: true, around: "ev_1", limit: 80 });
    await api.eventsPage({ conversation: true, personOnly: true, after: 42 });
    await api.searchConversation("部署 方案", 99);
    const urls = (fetchMock.mock.calls as unknown as [string][]).map(([url]) => url);
    expect(urls[0]).toBe("/api/events?page=1&conversation=1&around=ev_1&limit=80");
    expect(urls[1]).toBe("/api/events?page=1&conversation=1&origin=person&after=42&limit=50");
    expect(urls[2]).toBe(`/api/conversation/search?q=${encodeURIComponent("部署 方案").replace(/%20/g, "+")}&limit=20&before=99`);
  });
});
