import { appendEvent } from "../kernel/events.js";

/**
 * Active connector: periodic heartbeat. Proves the observe path stays alive;
 * triage rules keep it record-only so no model is woken.
 */
export function startHeartbeat(intervalSec: number): () => void {
  const emit = () =>
    appendEvent({
      source: "connector:timer",
      kind: "connector.heartbeat",
      payload: { at: new Date().toISOString() },
    }).catch((err) => console.error("heartbeat append failed:", err));
  const handle = setInterval(emit, intervalSec * 1000);
  void emit();
  return () => clearInterval(handle);
}
