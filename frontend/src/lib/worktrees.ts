import { ApiError, type CheckoutView } from "./api.js";

export type CheckoutBadgeKey = "running" | "archived" | "missing" | "repoMissing" | "setup" | "setupFailed" | "done" | "closed";

export interface CheckoutBadge {
  key: CheckoutBadgeKey;
  tone: "default" | "success" | "danger" | "muted";
}

/**
 * What a worktree row says about its state. An open task's idle worktree is
 * the ordinary case and carries no badge; trouble (the directory or the
 * repository gone, a failed setup) always shows.
 */
export function checkoutBadges(c: CheckoutView): CheckoutBadge[] {
  if (c.status === "archived") return [{ key: "archived", tone: "muted" }];
  const out: CheckoutBadge[] = [];
  if (c.health === "repo_missing") out.push({ key: "repoMissing", tone: "danger" });
  else if (c.health === "missing") out.push({ key: "missing", tone: "danger" });
  if (c.setup === "running") out.push({ key: "setup", tone: "default" });
  else if (c.setup === "failed") out.push({ key: "setupFailed", tone: "danger" });
  if (c.running) out.push({ key: "running", tone: "default" });
  else if (c.itemStatus === "done") out.push({ key: "done", tone: "success" });
  else if (c.itemStatus === "closed") out.push({ key: "closed", tone: "muted" });
  return out;
}

export interface Conflict {
  /** Uncommitted changes that archiving would delete. */
  dirty?: number;
  /** The task is running; it must be stopped first. */
  running?: boolean;
  /** A repository still has worktrees. */
  inUse?: boolean;
}

/** Why the server refused an archive or a removal, from its 409 body; null for any other failure. */
export function conflictOf(error: unknown): Conflict | null {
  if (!(error instanceof ApiError) || error.status !== 409) return null;
  let body: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(error.message.replace(/^\d+\s*/, "")) as unknown;
    if (parsed && typeof parsed === "object") body = parsed as Record<string, unknown>;
  } catch {
    return {};
  }
  const out: Conflict = {};
  if (typeof body["dirty"] === "number") out.dirty = body["dirty"];
  if (body["running"] === true) out.running = true;
  if (body["inUse"] === true) out.inUse = true;
  return out;
}
