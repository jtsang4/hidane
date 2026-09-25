import { sql, SAID_SQL } from "../kernel/db.js";
import {
  listEvents,
  REDACTED_SQL,
  SELECT_COLS,
  toEvent,
  type EventRow,
  type HidaneEvent,
} from "../kernel/events.js";
import { listWorkItems, type WorkItem } from "../kernel/workItems.js";

/**
 * The conversation as history: search, a day index and the window the Primary
 * reads. Derived from the log on every call — nothing here is stored.
 */

/** Kinds that carry words someone said in the conversation. */
const SAID_KINDS = `('user.message', 'agent.reply', 'escalation')`;

/**
 * What the conversation view renders, as a predicate: the person's messages
 * and questions on the main thread, plus answers from any thread that name
 * the message they answer. A subtask's report to its parent is not shown.
 */
const VISIBLE_SQL = `(kind IN ${SAID_KINDS}
  AND (thread_id = 'main' OR (kind = 'agent.reply' AND payload->>'root' IS NOT NULL))
  AND coalesce(payload->>'child', '') <> 'true')`;

const MAX_TERMS = 8;

/** Whitespace-separated terms, all of which must match (AND), case-insensitively. */
export function searchTerms(query: string): string[] {
  return query
    .split(/\s+/)
    .map((term) => term.trim().slice(0, 100))
    .filter(Boolean)
    .slice(0, MAX_TERMS);
}

function likePattern(term: string): string {
  return `%${term.replace(/[\\%_]/g, (c) => `\\${c}`)}%`;
}

export interface SearchPage {
  events: HidaneEvent[];
  hasMore: boolean;
}

/**
 * Newest-first search over everything ever said, not just what a reader has
 * loaded. Hidden messages never match, whatever their original words.
 */
export async function searchConversation(opts: {
  query: string;
  before?: number | undefined;
  limit?: number | undefined;
}): Promise<SearchPage> {
  const terms = searchTerms(opts.query);
  if (terms.length === 0) return { events: [], hasMore: false };
  const limit = Math.min(Math.max(opts.limit ?? 20, 1), 100);
  const params: unknown[] = [];
  const where = [VISIBLE_SQL, `NOT ${REDACTED_SQL}`];
  for (const term of terms) {
    params.push(likePattern(term));
    where.push(`${SAID_SQL} ILIKE $${params.length}`);
  }
  if (opts.before !== undefined && Number.isFinite(opts.before)) {
    params.push(opts.before);
    where.push(`seq < $${params.length}`);
  }
  const rows = await sql().unsafe(
    `SELECT ${SELECT_COLS} FROM events WHERE ${where.join(" AND ")}
     ORDER BY seq DESC LIMIT ${limit + 1}`,
    params as never[],
  );
  const events = (rows as unknown as EventRow[]).map(toEvent);
  return { events: events.slice(0, limit), hasMore: events.length > limit };
}

/** Work items (any status) whose title or id holds every term. */
export async function searchWorkItems(query: string, limit = 8): Promise<WorkItem[]> {
  const terms = searchTerms(query).map((t) => t.toLowerCase());
  if (terms.length === 0) return [];
  const items = await listWorkItems();
  return items
    .filter((item) => {
      const haystack = `${item.id} ${item.title}`.toLowerCase();
      return terms.every((term) => haystack.includes(term));
    })
    .reverse()
    .slice(0, limit);
}

export interface ConversationDay {
  /** `YYYY-MM-DD` in the requested timezone. */
  day: string;
  /** Messages and answers said that day. */
  count: number;
  /** The day's first said event — where a jump to the day lands. */
  firstId: string;
}

/** A valid IANA zone, or UTC. The name reaches SQL only as a parameter. */
export function safeZone(zone: string | undefined): string {
  if (!zone) return "UTC";
  try {
    new Intl.DateTimeFormat("en", { timeZone: zone });
    return zone;
  } catch {
    return "UTC";
  }
}

/** Every day something was said, newest first. */
export async function conversationDays(zone: string | undefined): Promise<ConversationDay[]> {
  const rows = await sql().unsafe(
    `SELECT to_char(ts AT TIME ZONE $1, 'YYYY-MM-DD') AS day,
            count(*)::int AS count,
            (array_agg(id ORDER BY seq))[1] AS first_id
     FROM events WHERE ${VISIBLE_SQL}
     GROUP BY 1 ORDER BY 1 DESC`,
    [safeZone(zone)],
  );
  return (rows as unknown as { day: string; count: number; first_id: string }[]).map((r) => ({
    day: r.day,
    count: Number(r.count),
    firstId: r.first_id,
  }));
}

/**
 * Titles for every work item the events mention. A reader cannot derive them
 * from what it has loaded: an item closed long ago is on no board, and the
 * message that named it may be pages away.
 */
export async function titlesFor(events: readonly HidaneEvent[]): Promise<Record<string, string>> {
  const ids = new Set<string>();
  for (const e of events) {
    if (e.workItemId) ids.add(e.workItemId);
    const named = e.payload["workItemId"];
    if (typeof named === "string" && named) ids.add(named);
  }
  if (ids.size === 0) return {};
  const rows = await sql()`SELECT id, title FROM work_items WHERE id = ANY(${[...ids]})`;
  return Object.fromEntries((rows as unknown as { id: string; title: string }[]).map((r) => [r.id, r.title]));
}

function clip(text: string, max: number): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > max ? `${flat.slice(0, max)}…` : flat;
}

function stamp(iso: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(iso));
}

interface ContextTurn {
  root: string;
  seq: number;
  ts: string;
  said: string | null;
  origin: string | null;
  routed: string | null;
  answers: string[];
  hidden: boolean;
}

/** How much of the conversation the Primary reads each turn. */
export const CONTEXT_TURNS = 12;
export const CONTEXT_CHARS = 6000;

export interface RecentConversation {
  /** Prompt section, or "" when nothing earlier was said. */
  text: string;
  /** Root of the oldest turn included; everything before it is out of view. */
  fromId: string | null;
  turns: number;
}

/**
 * The Primary's view of what was said before this turn: the newest turns of
 * the conversation, bounded by count and size, rebuilt from the log each time.
 *
 * This replaces an ever-growing model session as the Primary's memory of the
 * conversation. It is the same before and after a restart, it never needs a
 * compaction pass inside someone's turn, and what it covers can be shown to
 * the person, because the view calls this same function.
 */
export async function recentConversation(
  opts: { exclude?: ReadonlySet<string>; maxTurns?: number; maxChars?: number } = {},
): Promise<RecentConversation> {
  const maxTurns = opts.maxTurns ?? CONTEXT_TURNS;
  const maxChars = opts.maxChars ?? CONTEXT_CHARS;
  const events = await listEvents({ conversation: true, tail: 400 });
  const turns = new Map<string, ContextTurn>();
  const turnFor = (root: string, e: HidaneEvent): ContextTurn => {
    let turn = turns.get(root);
    if (!turn) {
      turn = { root, seq: e.seq, ts: e.ts, said: null, origin: null, routed: null, answers: [], hidden: false };
      turns.set(root, turn);
    }
    return turn;
  };
  for (const e of events) {
    const of = typeof e.payload["of"] === "string" ? e.payload["of"] : undefined;
    const root = typeof e.payload["root"] === "string" ? e.payload["root"] : undefined;
    if (e.kind === "user.message" && e.threadId === "main") {
      const turn = turnFor(e.id, e);
      turn.seq = e.seq;
      turn.ts = e.ts;
      if (e.payload["redacted"] === true) turn.hidden = true;
      else turn.said = String(e.payload["text"] ?? "");
    } else if (e.kind === "message.redacted" && of) {
      turnFor(of, e).hidden = true;
    } else if (e.kind === "message.attributed" && of) {
      turnFor(of, e).routed = `${String(e.payload["workItemId"] ?? "")} "${String(e.payload["title"] ?? "")}"`;
    } else if ((e.kind === "agent.reply" || e.kind === "escalation" || e.kind === "agent.error") && root) {
      if (e.payload["child"] === true) continue;
      const turn = turnFor(root, e);
      const kindLabel = e.payload["rootKind"];
      if (!turn.said && (kindLabel === "external" || kindLabel === "scheduled")) {
        turn.origin = `${kindLabel}: ${String(e.payload["rootText"] ?? "")}`;
      }
      const words = e.kind === "escalation" ? e.payload["question"] : e.payload["text"] ?? e.payload["error"];
      if (typeof words === "string" && words.trim()) {
        turn.answers.push(`${e.kind === "escalation" ? "question" : "answer"}${e.workItemId ? ` (${e.workItemId})` : ""}: ${clip(words, 400)}`);
      }
    }
  }
  const ordered = [...turns.values()]
    .filter((t) => !t.hidden && !opts.exclude?.has(t.root) && (t.said !== null || t.origin !== null))
    .sort((a, b) => a.seq - b.seq);
  const picked: string[] = [];
  let used = 0;
  let fromId: string | null = null;
  for (let i = ordered.length - 1; i >= 0 && picked.length < maxTurns; i--) {
    const t = ordered[i]!;
    const lines = [
      `[${t.root}] ${stamp(t.ts)} ${t.said !== null ? `person: ${clip(t.said, 300)}` : `(${clip(t.origin ?? "", 200)})`}`,
      ...(t.routed ? [`  → work item ${t.routed}`] : []),
      ...t.answers.slice(-2).map((a) => `  → ${a}`),
    ].join("\n");
    if (picked.length > 0 && used + lines.length > maxChars) break;
    picked.unshift(lines);
    used += lines.length;
    fromId = t.root;
  }
  if (picked.length === 0) return { text: "", fromId: null, turns: 0 };
  return {
    text: `Recent conversation (oldest first; already answered — context only, do not answer again):\n${picked.join("\n")}`,
    fromId,
    turns: picked.length,
  };
}

/** Search results as a Primary reads them when it asked to look further back. */
export function describeRecall(events: readonly HidaneEvent[]): string {
  if (events.length === 0) return "(nothing in the conversation history matched)";
  return [...events]
    .sort((a, b) => a.seq - b.seq)
    .map((e) => {
      const who = e.kind === "user.message" ? "person" : e.kind === "escalation" ? "question" : "answer";
      const words = e.kind === "escalation" ? e.payload["question"] : e.payload["text"];
      return `- ${e.ts.slice(0, 10)} ${who}${e.workItemId ? ` (${e.workItemId})` : ""}: ${clip(String(words ?? ""), 400)}`;
    })
    .join("\n");
}
