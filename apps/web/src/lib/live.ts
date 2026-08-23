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
