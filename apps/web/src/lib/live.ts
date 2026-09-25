export type LiveState = "connecting" | "live" | "offline";

/**
 * How long silence is tolerated before the stream counts as lost.
 * The server pings every 15s, so this allows two misses.
 */
export const STALE_AFTER_MS = 40_000;

/** How long to wait between reconnect attempts once the stream stops talking. */
export const RECONNECT_AFTER_MS = 20_000;

/**
 * Liveness cannot be read from EventSource error events: when the server dies
 * the socket can stay `readyState: OPEN` indefinitely with no error fired, so
 * the UI would keep showing stale data as though it were current. Judge it by
 * silence instead — the server sends a periodic ping precisely for this.
 *
 * `lastHeardAt` deliberately spans reconnect attempts. Restarting the clock on
 * each attempt made a permanently dead server read as "connecting" forever,
 * because re-dialling every 20s never let a 40s staleness window elapse.
 */
export function livenessFrom(
  lastHeardAt: number,
  everHeard: boolean,
  now: number,
  staleAfterMs = STALE_AFTER_MS,
): LiveState {
  if (now - lastHeardAt > staleAfterMs) return "offline";
  return everHeard ? "live" : "connecting";
}

/**
 * Should we tear the stream down and dial again?
 *
 * The same silent-death property means the browser will not reconnect for us:
 * it believes the zombie connection is fine, so a server that came back would
 * never be noticed. Both clocks matter — retry only when the stream has gone
 * quiet *and* the current attempt has had time to prove itself.
 */
export function shouldReconnect(
  lastHeardAt: number,
  connectedAt: number,
  now: number,
  reconnectAfterMs = RECONNECT_AFTER_MS,
): boolean {
  return now - lastHeardAt > reconnectAfterMs && now - connectedAt > reconnectAfterMs;
}

const CONVERSATION = new Set([
  "user.message",
  "agent.reply",
  "agent.error",
  "escalation",
  "message.attributed",
  "attribution.ambiguous",
  "execution.steered",
  "message.redacted",
]);

/**
 * Which cached queries one log event can have made stale.
 *
 * Invalidating everything on every event refetched every open list per tool
 * call once workers ran — a busy execution emits side effects several times a
 * second. The board and the lists are refreshed on a throttle by the caller;
 * the conversation and the item it concerns right away, since that is what the
 * reader is looking at.
 */
export function invalidationFor(event: {
  kind: string;
  threadId: string | null;
  workItemId: string | null;
}): { now: string[][]; throttled: string[][] } {
  const now: string[][] = [];
  const throttled: string[][] = [["board"], ["status"], ["events"], ["worklog"]];
  if (CONVERSATION.has(event.kind) && (event.threadId === "main" || event.kind === "agent.reply" || event.kind === "execution.steered")) {
    now.push(["conversation"]);
  }
  if (event.workItemId) now.push(["item", event.workItemId]);
  if (event.kind.startsWith("work_item.") || event.kind.startsWith("execution.")) {
    throttled.push(["items"]);
  }
  if (event.workItemId && event.kind === "execution.finished") throttled.push(["files", event.workItemId]);
  if (event.kind.startsWith("schedule.") || event.kind === "connector.http") {
    throttled.push(["schedules"], ["schedule-runs"]);
  }
  if (event.kind.startsWith("memory.")) throttled.push(["memories"]);
  if (event.kind.startsWith("policy.")) throttled.push(["policies"]);
  return { now, throttled };
}
