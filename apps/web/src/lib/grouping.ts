import type { HidaneEvent } from "./api.js";

export interface ExecutionGroup {
  executionId: string;
  started?: HidaneEvent | undefined;
  finished?: HidaneEvent | undefined;
  sideEffects: HidaneEvent[];
  ok: boolean | null;
}

/** Conversation events shown as chat bubbles. */
export function conversationEvents(events: HidaneEvent[]): HidaneEvent[] {
  return events.filter((e) =>
    ["user.message", "agent.reply", "escalation", "agent.error"].includes(e.kind),
  );
}

/** Group execution lifecycle events into per-execution timelines. */
export function executionGroups(events: HidaneEvent[]): ExecutionGroup[] {
  const groups = new Map<string, ExecutionGroup>();
  for (const e of events) {
    if (!e.executionId) continue;
    let g = groups.get(e.executionId);
    if (!g) {
      g = { executionId: e.executionId, sideEffects: [], ok: null };
      groups.set(e.executionId, g);
    }
    if (e.kind === "execution.started") g.started = e;
    else if (e.kind === "execution.finished") {
      g.finished = e;
      g.ok = e.payload["ok"] === true;
    } else if (e.kind.startsWith("side_effect.")) g.sideEffects.push(e);
  }
  return [...groups.values()].sort(
    (a, b) => (a.started?.seq ?? 0) - (b.started?.seq ?? 0),
  );
}

export function payloadText(e: HidaneEvent): string {
  const t = e.payload["text"] ?? e.payload["note"] ?? e.payload["error"];
  return typeof t === "string" ? t : JSON.stringify(e.payload);
}
