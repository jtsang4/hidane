import { appendEvent, commitCursor, nextBatch } from "../kernel/events.js";
import { needsTriage, triageEvent } from "../kernel/triage.js";
import { handleUserMessage } from "../agents/primary.js";

const CONSUMER = "triage";

/**
 * Background lane: connector events are consumed off the log with a cursor,
 * pass deterministic rules first, and only rarely wake the Primary.
 */
export async function triageOnce(): Promise<{ handled: number; woke: number }> {
  const batch = await nextBatch(CONSUMER, 50);
  let woke = 0;
  let last = 0;
  for (const event of batch) {
    last = event.seq;
    if (!needsTriage(event)) continue;
    const { rule, action } = triageEvent(event);
    await appendEvent({
      source: "kernel:triage",
      kind: "triage.decision",
      payload: { of: event.id, ofKind: event.kind, rule, action },
    });
    if (action === "wake_primary") {
      woke++;
      const summary = `External event ${event.kind} from ${event.source}: ${JSON.stringify(event.payload).slice(0, 1500)}`;
      await handleUserMessage(summary, event.source);
    }
  }
  if (batch.length > 0) await commitCursor(CONSUMER, last);
  return { handled: batch.length, woke };
}

export function startTriageLoop(intervalSec: number): () => void {
  let running = false;
  const tick = async () => {
    if (running) return;
    running = true;
    try {
      await triageOnce();
    } catch (err) {
      console.error("triage loop error:", err);
    } finally {
      running = false;
    }
  };
  const handle = setInterval(tick, intervalSec * 1000);
  void tick();
  return () => clearInterval(handle);
}
