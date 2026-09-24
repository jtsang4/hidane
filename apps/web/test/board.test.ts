import { describe, expect, it } from "vitest";
import type { BoardCard, CardState } from "../src/lib/api.js";
import { isUnread, trayCards } from "../src/lib/board.js";
import { addNotice, digest, dropNotices, noticeFor } from "../src/lib/notices.js";
import { invalidationFor } from "../src/lib/live.js";
import { focusFrom, focusHref, routeFor } from "../src/lib/router.svelte.js";

function card(id: string, state: CardState, lastSeq = 10, status: "open" | "done" | "closed" = "open"): BoardCard {
  return {
    item: {
      id,
      title: id,
      status,
      workspace: "/w",
      threadId: `th_${id}`,
      parentId: null,
      deadlineAt: null,
      createdAt: `2026-09-24T10:00:0${id.length}Z`,
      updatedAt: "2026-09-24T10:00:00Z",
    },
    state,
    understanding: null,
    lastReply: null,
    execution: null,
    escalation: null,
    lastPolicyBlock: null,
    anchor: null,
    childIds: [],
    lastSeq,
  };
}

describe("task tray", () => {
  it("shows what is moving or waiting, waiting first, plus unread idle work", () => {
    const cards = [card("a", "idle", 5), card("bb", "running"), card("ccc", "waiting"), card("dddd", "idle", 20)];
    const seen = { a: 5, dddd: 10 };
    expect(trayCards(cards, seen).map((c) => c.item.id)).toEqual(["ccc", "bb", "dddd"]);
  });

  it("treats a card never opened as read — nothing new since the person caused it", () => {
    expect(isUnread(card("a", "idle", 9), {})).toBe(false);
    expect(isUnread(card("a", "idle", 9), { a: 3 })).toBe(true);
  });
});

describe("notices", () => {
  it("keep one line per turn and summarise past a handful", () => {
    let notices = [] as ReturnType<typeof digest>["shown"];
    for (const [root, seq] of [["r1", 1], ["r2", 2], ["r1", 3], ["r3", 4], ["r4", 5]] as const) {
      notices = addNotice(notices, { root, workItemId: null, kind: "reply", seq });
    }
    expect(notices.map((n) => n.root)).toEqual(["r2", "r1", "r3", "r4"]);
    const view = digest(notices);
    expect(view.shown.map((n) => n.root)).toEqual(["r1", "r3", "r4"]);
    expect(view.hidden).toBe(1);
    expect(dropNotices(notices, new Set(["r1"])).map((n) => n.root)).toEqual(["r2", "r3", "r4"]);
  });

  it("come only from answers that name their turn", () => {
    const base = { seq: 1, id: "e", ts: "", source: "s", threadId: "main", workItemId: "wi_a", executionId: null };
    expect(noticeFor({ ...base, kind: "agent.reply", payload: { root: "m1" } })?.kind).toBe("reply");
    expect(noticeFor({ ...base, kind: "escalation", payload: { root: "m1" } })?.kind).toBe("question");
    expect(noticeFor({ ...base, kind: "side_effect.intent", payload: { root: "m1" } })).toBeNull();
  });
});

describe("live invalidation", () => {
  it("refreshes the conversation and the item now, lists on a throttle", () => {
    const reply = invalidationFor({ kind: "agent.reply", threadId: "th_a", workItemId: "wi_a" });
    expect(reply.now).toEqual([["conversation"], ["item", "wi_a"]]);
    const tool = invalidationFor({ kind: "side_effect.intent", threadId: "th_a", workItemId: "wi_a" });
    expect(tool.now).toEqual([["item", "wi_a"]]);
    expect(tool.throttled).toContainEqual(["board"]);
    expect(tool.throttled).not.toContainEqual(["items"]);
  });
});

describe("focus routing", () => {
  it("opens a work item beside the conversation through the query string", () => {
    expect(focusFrom("?focus=wi_a")).toBe("wi_a");
    expect(focusFrom("")).toBeNull();
    expect(focusHref("wi_a")).toBe("/?focus=wi_a");
    expect(routeFor("/policies")).toEqual({ name: "policies" });
  });
});
