import type { CardState, WorkItemStatus } from "./api.js";
import { ACTIVE } from "./board.js";

/**
 * The composer's addressing: `@` names the tasks a message goes to, `/` runs
 * a command on them. Kept apart from the component so it can be tested
 * without a browser.
 */

/** An `@` being typed: the range it covers in the text and what follows it. */
export interface MentionQuery {
  start: number;
  end: number;
  query: string;
}

/** The `@` word the caret is in: an `@` at the start or after a space, with no space typed since. */
export function mentionAt(text: string, caret: number): MentionQuery | null {
  const before = text.slice(0, caret);
  const match = /(^|\s)@([^\s@]*)$/.exec(before);
  if (!match) return null;
  const query = match[2] ?? "";
  return { start: caret - query.length - 1, end: caret, query };
}

/** The text with a range cut out, and where the caret goes. */
export function cutRange(text: string, start: number, end: number): { text: string; caret: number } {
  let tail = text.slice(end);
  // The word is gone; the space that ended it should not linger before the next one.
  if (tail.startsWith(" ") && (start === 0 || text[start - 1] === " ")) tail = tail.slice(1);
  return { text: text.slice(0, start) + tail, caret: start };
}

export interface Candidate {
  id: string;
  title: string;
  status: WorkItemStatus;
  /** On the board: what it is doing now. */
  state: CardState | null;
  updatedAt: string;
}

/**
 * Tasks to offer for an `@`: what waits on the person first, then what is
 * moving, then the rest by recency. Archived tasks only when searched for.
 */
export function rankCandidates(candidates: readonly Candidate[], query: string, exclude: ReadonlySet<string>, limit = 8): Candidate[] {
  const q = query.trim().toLowerCase();
  const rank = (c: Candidate): number => {
    if (c.status === "closed") return 5;
    if (c.state === "waiting") return 0;
    if (c.state === "review") return 1;
    if (c.state && ACTIVE.has(c.state)) return 2;
    return c.status === "open" ? 3 : 4;
  };
  const position = (c: Candidate): number => {
    if (!q) return 0;
    const at = c.title.toLowerCase().indexOf(q);
    return at === 0 ? 0 : at > 0 ? 1 : 2;
  };
  return candidates
    .filter((c) => !exclude.has(c.id))
    .filter((c) => (q === "" ? c.status !== "closed" : c.title.toLowerCase().includes(q) || c.id.toLowerCase().includes(q)))
    .sort((a, b) => position(a) - position(b) || rank(a) - rank(b) || b.updatedAt.localeCompare(a.updatedAt))
    .slice(0, limit);
}

const SLASH_COMMANDS = ["stop", "done", "archive", "reopen"] as const;
export type SlashCommand = (typeof SLASH_COMMANDS)[number];

/** The command being typed, while the whole message so far is `/` and letters. */
export function slashAt(text: string, caret: number): MentionQuery | null {
  const match = /^\/([a-z]*)$/i.exec(text.slice(0, caret));
  if (!match || text.slice(caret).trim() !== "") return null;
  return { start: 0, end: caret, query: (match[1] ?? "").toLowerCase() };
}

export function matchCommands(prefix: string): SlashCommand[] {
  return SLASH_COMMANDS.filter((command) => command.startsWith(prefix.toLowerCase()));
}

/** A message that is exactly one command; anything else (even `/etc/hosts …`) is said, not run. */
export function parseSlash(text: string): SlashCommand | null {
  const match = /^\/([a-z]+)$/i.exec(text.trim());
  const word = match?.[1]?.toLowerCase();
  return SLASH_COMMANDS.find((command) => command === word) ?? null;
}
