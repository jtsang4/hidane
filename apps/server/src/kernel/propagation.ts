/**
 * Which facts travel up the work tree. Like DOM events, bubbling is declared
 * per kind rather than decided per emit: progress and tool traffic stay where
 * they happen (log and UI only), because waking a parent's model for each of
 * them would cost a model call per tool call. Only what the current level
 * could not settle — a question, a misrouted message, a refused action —
 * climbs, one level at a time, until some handler stops it.
 */
const BUBBLING_KINDS = new Set(["escalation.raised", "message.reroute_requested"]);

export function bubbles(kind: string): boolean {
  return BUBBLING_KINDS.has(kind);
}
