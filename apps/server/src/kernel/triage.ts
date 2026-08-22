import type { HidaneEvent } from "./events.js";

export type TriageAction = "record" | "wake_primary";

export interface TriageRule {
  name: string;
  match: (event: HidaneEvent) => boolean;
  action: TriageAction;
}

/**
 * Deterministic rules run before any model is woken.
 * Most connector events should end here as "record".
 */
export const DEFAULT_RULES: TriageRule[] = [
  {
    name: "heartbeat-record-only",
    match: (e) => e.kind === "connector.heartbeat",
    action: "record",
  },
  {
    name: "webhook-wakes-primary",
    match: (e) => e.kind === "connector.webhook",
    action: "wake_primary",
  },
];

export function triageEvent(
  event: HidaneEvent,
  rules: TriageRule[] = DEFAULT_RULES,
): { rule: string; action: TriageAction } {
  for (const rule of rules) {
    if (rule.match(event)) return { rule: rule.name, action: rule.action };
  }
  return { rule: "default-record", action: "record" };
}

/** Only connector-sourced events go through triage; agent/kernel events never re-trigger. */
export function needsTriage(event: HidaneEvent): boolean {
  return event.source.startsWith("connector:") && event.kind.startsWith("connector.");
}
