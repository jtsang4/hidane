import type { BoardCard, CardState } from "./api.js";

/** States where the work item is doing something or needs the person. */
const ACTIVE: ReadonlySet<CardState> = new Set(["waiting", "running", "queued", "thinking", "delegated"]);

/** Badge tone per state; waiting outranks everything because it needs a person. */
export function stateTone(state: CardState): "default" | "success" | "danger" | "muted" {
  if (state === "waiting") return "danger";
  if (state === "running" || state === "thinking" || state === "queued" || state === "delegated") return "default";
  if (state === "done") return "success";
  return "muted";
}

export function isActive(card: BoardCard): boolean {
  return ACTIVE.has(card.state);
}

/** New activity the person has not looked at since they last opened the card. */
export function isUnread(card: BoardCard, seen: Readonly<Record<string, number>>): boolean {
  const last = seen[card.item.id];
  // Never opened: only its own creation happened, which the person caused.
  if (last === undefined) return false;
  return card.lastSeq > last;
}

/**
 * The tabs along the top: whatever is in motion or waiting, plus finished work
 * the person has not looked at yet. Waiting first — it is blocked on them.
 */
export function trayCards(cards: BoardCard[], seen: Readonly<Record<string, number>>): BoardCard[] {
  const rank = (c: BoardCard) => (c.state === "waiting" ? 0 : isActive(c) ? 1 : 2);
  return cards
    .filter((c) => c.item.status === "open" && (isActive(c) || isUnread(c, seen)))
    .sort((a, b) => rank(a) - rank(b) || a.item.createdAt.localeCompare(b.item.createdAt));
}

const SEEN_KEY = "hidane-seen";

export function loadSeen(): Record<string, number> {
  if (typeof localStorage === "undefined") return {};
  try {
    const parsed = JSON.parse(localStorage.getItem(SEEN_KEY) ?? "{}") as unknown;
    return parsed && typeof parsed === "object" ? (parsed as Record<string, number>) : {};
  } catch {
    return {};
  }
}

export function saveSeen(seen: Record<string, number>): void {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(SEEN_KEY, JSON.stringify(seen));
}
