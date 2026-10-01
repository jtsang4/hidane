export interface Completion {
  kind: string;
  workItemId: string | null;
  /** Short line suitable for a notification body. */
  summary: string;
  ok: boolean;
}

/** Only terminal outcomes are worth interrupting someone for. */
const ALERT_KINDS = new Set(["execution.finished", "escalation"]);

/**
 * Decide whether an SSE payload is a completion worth announcing.
 *
 * Executions routinely run for minutes; with nothing to signal the end, you
 * either sit and watch the page or switch away and never learn it finished —
 * the same blind spot that had a finished run mistaken for a hang.
 */
export function completionFrom(raw: string): Completion | null {
  let event: Record<string, unknown>;
  try {
    event = JSON.parse(raw) as Record<string, unknown>;
  } catch {
    return null;
  }
  const kind = typeof event["kind"] === "string" ? event["kind"] : "";
  if (!ALERT_KINDS.has(kind)) return null;
  const payload = (event["payload"] ?? {}) as Record<string, unknown>;
  const ok = kind === "escalation" ? true : payload["ok"] === true;
  const raw_summary =
    (typeof payload["summary"] === "string" && payload["summary"]) ||
    (typeof payload["note"] === "string" && payload["note"]) ||
    (typeof payload["error"] === "string" && payload["error"]) ||
    "";
  return {
    kind,
    workItemId: typeof event["workItemId"] === "string" ? event["workItemId"] : null,
    summary: raw_summary.replace(/\s+/g, " ").slice(0, 120),
    ok,
  };
}

/** Unread marker in the tab title, so a background tab still says something. */
export function badgeTitle(base: string, unseen: number): string {
  return unseen > 0 ? `(${unseen}) ${base}` : base;
}

export type NotifyPermission = "default" | "granted" | "denied" | "unsupported";

export function notifyPermission(): NotifyPermission {
  if (typeof Notification === "undefined") return "unsupported";
  return Notification.permission as NotifyPermission;
}

/** Asking must follow a click: browsers reject permission prompts otherwise. */
export async function requestNotifyPermission(): Promise<NotifyPermission> {
  if (typeof Notification === "undefined") return "unsupported";
  return (await Notification.requestPermission()) as NotifyPermission;
}
