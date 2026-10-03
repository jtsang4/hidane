import { describe, expect, it } from "vitest";
import { ApiError, type CheckoutView } from "../src/lib/api.js";
import { checkoutBadges, conflictOf } from "../src/lib/worktrees.js";

function view(patch: Partial<CheckoutView> = {}): CheckoutView {
  return {
    id: "co_1",
    workItemId: "wi_1",
    repoId: "repo_1",
    mode: "worktree",
    path: "/h/workspaces/wi_1/blog",
    branch: "hidane/wi_1",
    base: "main",
    status: "active",
    setup: "done",
    createdAt: "2026-10-01T00:00:00.000Z",
    updatedAt: "2026-10-01T00:00:00.000Z",
    repoName: "blog",
    repoPath: "/code/blog",
    repoStatus: "present",
    health: "ok",
    ahead: 0,
    dirty: 0,
    head: "hidane/wi_1",
    title: "RSS",
    itemStatus: "open",
    running: false,
    lastActivityAt: "2026-10-01T00:00:00.000Z",
    ...patch,
  };
}

describe("checkoutBadges", () => {
  it("leaves an open task's idle worktree unmarked", () => {
    expect(checkoutBadges(view())).toEqual([]);
  });
  it("says what is wrong before what is ordinary", () => {
    expect(checkoutBadges(view({ health: "repo_missing", running: true })).map((b) => b.key)).toEqual(["repoMissing", "running"]);
    expect(checkoutBadges(view({ setup: "failed", itemStatus: "done" })).map((b) => b.key)).toEqual(["setupFailed", "done"]);
  });
  it("shows an archived checkout as archived only", () => {
    expect(checkoutBadges(view({ status: "archived", running: true }))).toEqual([{ key: "archived", tone: "muted" }]);
  });
});

describe("conflictOf", () => {
  it("reads the server's reason for a 409", () => {
    expect(conflictOf(new ApiError(409, `409 {"ok":false,"error":"x","dirty":3}`))).toEqual({ dirty: 3 });
    expect(conflictOf(new ApiError(409, `409 {"ok":false,"running":true}`))).toEqual({ running: true });
    expect(conflictOf(new ApiError(409, `409 {"ok":false,"inUse":true}`))).toEqual({ inUse: true });
  });
  it("is null for anything that is not a conflict", () => {
    expect(conflictOf(new ApiError(500, "500 boom"))).toBeNull();
    expect(conflictOf(new Error("x"))).toBeNull();
  });
});
