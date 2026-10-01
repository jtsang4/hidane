import type { HidaneEvent } from "./api.js";

/**
 * Free-text match over an event, payload included.
 *
 * Deliberately client-side over already-loaded rows: the server filters are
 * indexed columns (kind, work item), while "the message where I mentioned the
 * deploy" lives in the payload. Searching there server-side would mean a scan
 * of the whole append-only log for every keystroke.
 *
 * All whitespace-separated terms must match (AND), case-insensitively.
 */
export function matchesQuery(event: HidaneEvent, query: string): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const haystack = [
    String(event.seq),
    event.kind,
    event.source,
    event.workItemId ?? "",
    event.threadId ?? "",
    event.executionId ?? "",
    JSON.stringify(event.payload),
  ]
    .join(" ")
    .toLowerCase();
  return terms.every((term) => haystack.includes(term));
}

/** Same AND-of-terms rule, over a work item's title and id. */
export function matchesItem(
  item: { id: string; title: string; status: string },
  query: string,
): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const haystack = `${item.id} ${item.title} ${item.status}`.toLowerCase();
  return terms.every((term) => haystack.includes(term));
}
