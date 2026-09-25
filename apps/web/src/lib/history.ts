import type { ConversationDay, HidaneEvent } from "./api.js";
import type { Turn } from "./conversation.js";

/**
 * Reading the conversation as history rather than as a feed: where a search
 * hit lives, how days divide it, and what a match looks like in context.
 */

/** Whitespace-separated terms; every one must match. Mirrors the server's rule. */
export function searchTerms(query: string): string[] {
  return query.split(/\s+/).map((t) => t.trim()).filter(Boolean).slice(0, 8);
}

/** The words a conversation event carries. */
export function saidText(event: HidaneEvent): string {
  const words = event.kind === "escalation" ? event.payload["question"] : event.payload["text"];
  return typeof words === "string" ? words : "";
}

export interface Segment {
  text: string;
  hit: boolean;
}

/** Text split into matched and unmatched runs, case-insensitively, for `<mark>`. */
export function highlight(text: string, terms: readonly string[]): Segment[] {
  const needles = terms.map((t) => t.toLowerCase()).filter(Boolean);
  if (needles.length === 0 || !text) return text ? [{ text, hit: false }] : [];
  const lower = text.toLowerCase();
  const hits: boolean[] = Array.from({ length: text.length }, () => false);
  for (const needle of needles) {
    for (let at = lower.indexOf(needle); at !== -1; at = lower.indexOf(needle, at + needle.length)) {
      for (let i = at; i < at + needle.length; i++) hits[i] = true;
    }
  }
  const segments: Segment[] = [];
  for (let i = 0; i < text.length; i++) {
    const last = segments.at(-1);
    if (last && last.hit === hits[i]) last.text += text[i];
    else segments.push({ text: text[i]!, hit: hits[i]! });
  }
  return segments;
}

/**
 * A short excerpt around the first match, so a hit in the middle of a long
 * reply is visible without opening it.
 */
export function excerpt(text: string, terms: readonly string[], radius = 60): string {
  const flat = text.replace(/\s+/g, " ").trim();
  const lower = flat.toLowerCase();
  const first = terms
    .map((t) => lower.indexOf(t.toLowerCase()))
    .filter((i) => i >= 0)
    .sort((a, b) => a - b)[0];
  if (first === undefined || flat.length <= radius * 2) {
    return flat.length > radius * 2 ? `${flat.slice(0, radius * 2)}…` : flat;
  }
  const start = Math.max(0, first - radius);
  const end = Math.min(flat.length, first + radius);
  return `${start > 0 ? "…" : ""}${flat.slice(start, end)}${end < flat.length ? "…" : ""}`;
}

/** The turn an event belongs to: a person's message is its own root. */
export function rootOfEvent(event: HidaneEvent): string {
  if (event.kind === "user.message" && event.threadId === "main") return event.id;
  const root = event.payload["root"] ?? event.payload["of"];
  return typeof root === "string" && root ? root : event.id;
}

/** When a turn happened: its message, or the first sign of it that is loaded. */
export function turnTime(turn: Turn): string | null {
  return turn.message?.ts ?? turn.attribution?.ts ?? turn.answers[0]?.ts ?? turn.ambiguous?.ts ?? null;
}

/** `YYYY-MM-DD` of an instant in a timezone (the reader's by default). */
export function dayOf(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    ...(timeZone ? { timeZone } : {}),
  }).format(new Date(iso));
}

/**
 * Roots of the turns that open a new day, with that day. The first loaded turn
 * gets one too: a window opened in the middle of history must say where it is.
 */
export function dayBreaks(turns: readonly Turn[], timeZone?: string): Map<string, string> {
  const breaks = new Map<string, string>();
  let previous: string | null = null;
  for (const turn of turns) {
    const ts = turnTime(turn);
    if (!ts) continue;
    const day = dayOf(ts, timeZone);
    if (day !== previous) breaks.set(turn.root, day);
    previous = day;
  }
  return breaks;
}

/** Days grouped under their month (`YYYY-MM`), keeping newest-first order. */
export function daysByMonth(days: readonly ConversationDay[]): { month: string; days: ConversationDay[] }[] {
  const groups: { month: string; days: ConversationDay[] }[] = [];
  for (const day of days) {
    const month = day.day.slice(0, 7);
    const group = groups.at(-1);
    if (group && group.month === month) group.days.push(day);
    else groups.push({ month, days: [day] });
  }
  return groups;
}

/** Oldest and newest seq of what is loaded. */
export function loadedRange(events: Iterable<{ seq: number }>): { oldest: number; newest: number } | null {
  let oldest = Infinity;
  let newest = -Infinity;
  for (const e of events) {
    if (e.seq < oldest) oldest = e.seq;
    if (e.seq > newest) newest = e.seq;
  }
  return Number.isFinite(oldest) ? { oldest, newest } : null;
}

/**
 * Whether the Primary's view begins inside what is loaded — only then is there
 * a place to mark it, with older turns above it that the assistant does not see.
 */
export function contextBoundary(turns: readonly Turn[], fromId: string | null, hasOlder = false): string | null {
  if (!fromId) return null;
  const index = turns.findIndex((t) => t.root === fromId);
  return index > 0 || (index === 0 && hasOlder) ? fromId : null;
}
