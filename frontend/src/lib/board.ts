import type { BoardCard, CardState } from "./api.js";

/** States where the work item is doing something. */
const ACTIVE: ReadonlySet<CardState> = new Set(["running", "queued", "thinking", "delegated"]);

/** The dot that stands for each state, wherever a task is listed. */
export const STATE_DOT: Record<CardState, string> = {
  waiting: "bg-danger",
  review: "bg-primary",
  running: "animate-ember bg-primary",
  queued: "bg-primary/50",
  thinking: "animate-ember bg-primary/80",
  delegated: "animate-ember bg-primary/60",
  idle: "bg-muted",
  done: "bg-success",
  closed: "bg-muted",
};

/** Badge tone per state; waiting outranks everything because it needs a person. */
export function stateTone(state: CardState): "default" | "success" | "danger" | "muted" {
  if (state === "waiting") return "danger";
  if (state === "review" || state === "running" || state === "thinking" || state === "queued" || state === "delegated") return "default";
  if (state === "done") return "success";
  return "muted";
}

function isActive(card: BoardCard): boolean {
  return ACTIVE.has(card.state);
}

/** Blocked on the person: a question to answer, or a result to review. */
export function needsPerson(card: BoardCard): boolean {
  return card.item.status === "open" && (card.state === "waiting" || card.state === "review");
}

/**
 * The person's queue: questions first — work is stopped on them — then results
 * to review, the most recent first within each.
 */
export function attentionCards(cards: BoardCard[]): BoardCard[] {
  const rank = (c: BoardCard) => (c.state === "waiting" ? 0 : 1);
  return cards.filter(needsPerson).sort((a, b) => rank(a) - rank(b) || b.lastSeq - a.lastSeq);
}

/** New activity the person has not looked at since they last opened the card. */
export function isUnread(card: BoardCard, seen: Readonly<Record<string, number>>): boolean {
  const last = seen[card.item.id];
  // Never opened: only its own creation happened, which the person caused.
  if (last === undefined) return false;
  return card.lastSeq > last;
}

/**
 * The sidebar's in-progress list: whatever is in motion, plus finished work
 * the person has not looked at yet. What waits on the person is listed apart
 * (`attentionCards`), not here.
 */
export function trayCards(cards: BoardCard[], seen: Readonly<Record<string, number>>): BoardCard[] {
  const rank = (c: BoardCard) => (isActive(c) ? 0 : 1);
  return cards
    .filter((c) => c.item.status === "open" && !needsPerson(c) && (isActive(c) || isUnread(c, seen)))
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
