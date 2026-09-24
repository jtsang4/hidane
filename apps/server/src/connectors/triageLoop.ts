import { appendEvent, commitCursor, nextBatch } from "../kernel/events.js";
import { post } from "../kernel/mailbox.js";
import { onEventAppended } from "../kernel/notify.js";
import { needsTriage, triageEvent } from "../kernel/triage.js";
import { PRIMARY } from "../agents/addresses.js";

const CONSUMER = "triage";

/**
 * Background lane: connector events are consumed off the log with a cursor and
 * pass deterministic rules. A wake is a message posted to the Primary's
 * mailbox — the decision record doubles as the delivery — so triage never
 * waits for the Primary and one slow wake cannot hold up the rest.
 */
export async function triageOnce(): Promise<{ handled: number; woke: number }> {
  const batch = await nextBatch(CONSUMER, 50);
  let woke = 0;
  for (const event of batch) {
    if (needsTriage(event)) {
      const { rule, action } = triageEvent(event);
      const payload = { of: event.id, ofKind: event.kind, rule, action };
      if (action === "wake_primary") {
        woke++;
        await post({
          source: "kernel:triage",
          kind: "triage.decision",
          mailbox: PRIMARY,
          lane: "normal",
          causedBy: event,
          payload: {
            ...payload,
            summary: `External event ${event.kind} from ${event.source}: ${JSON.stringify(event.payload).slice(0, 1500)}`,
          },
        });
      } else {
        await appendEvent({ source: "kernel:triage", kind: "triage.decision", payload });
      }
    }
    await commitCursor(CONSUMER, event.seq);
  }
  return { handled: batch.length, woke };
}

export function startTriageLoop(intervalSec: number): () => void {
  let running = false;
  let again = false;
  const tick = async () => {
    if (running) {
      again = true;
      return;
    }
    running = true;
    try {
      do {
        again = false;
        const { handled } = await triageOnce();
        if (handled === 50) again = true;
      } while (again);
    } catch (err) {
      console.error("triage loop error:", err);
    } finally {
      running = false;
    }
  };
  const handle = setInterval(() => void tick(), intervalSec * 1000);
  // The loop's own triage.decision appends notify too; the cursor makes those
  // wake-ups cheap no-ops rather than a feedback loop.
  const unlisten = onEventAppended(() => void tick());
  void tick();
  return () => {
    clearInterval(handle);
    unlisten();
  };
}
