import type { AgentSession } from "@earendil-works/pi-coding-agent";
import { config } from "../config.js";
import { extractJson } from "./pi.js";
import { beginLiveText } from "./liveText.js";
import { createReplyExtractor } from "./replyStream.js";
import { promptRole } from "./sdk.js";
import type { InboundImage } from "./inbox.js";

export interface Effect {
  type: string;
  [key: string]: unknown;
}

export interface Thought {
  ok: boolean;
  /** Null when the answer was not the effect JSON the charter asks for. */
  effects: Effect[] | null;
  raw: string;
  error?: string | undefined;
  durationMs: number;
}

/**
 * The shared half of every agent loop: one model call over the turn's context,
 * streamed to a thread as it is written, parsed into effects. Roles differ in
 * the context they build and the effects they may emit — not in this.
 */
export async function think(
  session: AgentSession,
  prompt: string,
  opts: { images?: InboundImage[]; liveThreadId?: string | undefined } = {},
): Promise<Thought> {
  // Closed when the model stops, not when the effects are applied: holding the
  // provisional bubble open across that gap leaves a cursor blinking under
  // text that is already complete. The durable reply takes over from it.
  const live = opts.liveThreadId ? beginLiveText(opts.liveThreadId) : undefined;
  const extract = createReplyExtractor();
  const result = await promptRole(
    session,
    prompt,
    config.routeTimeoutSec,
    opts.images ?? [],
    live ? (delta) => live.push(extract(delta)) : undefined,
  ).finally(() => live?.end());
  if (!result.ok) {
    return { ok: false, effects: null, raw: "", error: result.error, durationMs: result.durationMs };
  }
  return {
    ok: true,
    effects: parseEffects(result.text),
    raw: result.text,
    durationMs: result.durationMs,
  };
}

export function parseEffects(text: string): Effect[] | null {
  const parsed = extractJson<Record<string, unknown>>(text);
  if (!parsed) return null;
  const list = Array.isArray(parsed["effects"])
    ? parsed["effects"]
    : typeof parsed["type"] === "string"
      ? [parsed]
      : null;
  if (!list) return null;
  return list.filter(
    (e): e is Effect => typeof e === "object" && e !== null && typeof (e as Effect).type === "string",
  );
}

export function str(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

/** The current local time, so a role can answer "what day is it" and reason about deadlines. */
export function nowLine(now = new Date()): string {
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const local = new Intl.DateTimeFormat("en-CA", {
    dateStyle: "full",
    timeStyle: "short",
    timeZone: zone,
  }).format(now);
  return `Now: ${local} (${zone})`;
}
