import { sql, type Sql } from "./db.js";

type Listener = (seq: number) => void;

const listeners = new Set<Listener>();
let listeningOn: Sql | undefined;

/**
 * Wake-ups for appended events, driven by the `events_notify` trigger.
 *
 * One LISTEN connection per process, shared by every subscriber. Delivery is a
 * hint, never the data: a subscriber must still read the log from its own
 * cursor, so a notification lost to a reconnect only costs latency until the
 * subscriber's fallback poll.
 */
export function onEventAppended(listener: Listener): () => void {
  listeners.add(listener);
  const db = sql();
  if (listeningOn !== db) {
    listeningOn = db;
    db.listen("hidane_events", (payload) => {
      const seq = Number(payload);
      for (const l of listeners) l(seq);
    }).catch((err: unknown) => {
      if (listeningOn === db) listeningOn = undefined;
      console.error("event LISTEN failed:", err);
    });
  }
  return () => {
    listeners.delete(listener);
  };
}
