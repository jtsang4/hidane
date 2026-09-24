import type { HidaneEvent } from "./api.js";
import { rootIdOf } from "./conversation.js";

/** Something answered off-screen, worth a line at the bottom of the conversation. */
export interface Notice {
  root: string;
  workItemId: string | null;
  kind: "reply" | "question" | "choice";
  seq: number;
}

const NOTICE_KINDS: Record<string, Notice["kind"]> = {
  "agent.reply": "reply",
  escalation: "question",
  "attribution.ambiguous": "choice",
};

/** A notice for this event, if it is an answer the person would want to see. */
export function noticeFor(event: HidaneEvent): Notice | null {
  const kind = NOTICE_KINDS[event.kind];
  const root = rootIdOf(event);
  if (!kind || !root || event.payload["child"] === true) return null;
  return { root, workItemId: event.workItemId, kind, seq: event.seq };
}

/** One notice per turn — the newest wins — so a chatty task cannot flood the bar. */
export function addNotice(notices: Notice[], notice: Notice): Notice[] {
  return [...notices.filter((n) => n.root !== notice.root), notice].sort((a, b) => a.seq - b.seq);
}

export function dropNotices(notices: Notice[], roots: ReadonlySet<string>): Notice[] {
  const next = notices.filter((n) => !roots.has(n.root));
  return next.length === notices.length ? notices : next;
}

/**
 * Backpressure on attention: past a handful of notices the bar stops listing
 * and summarises, the same way the tray stops at what is in motion.
 */
export function digest(notices: Notice[], limit = 3): { shown: Notice[]; hidden: number } {
  if (notices.length <= limit) return { shown: notices, hidden: 0 };
  return { shown: notices.slice(-limit), hidden: notices.length - limit };
}
