/**
 * Cursor for the next page when walking a log backwards.
 *
 * The API returns each page in ascending `seq` and pages are requested with an
 * exclusive `before`, so the cursor must be the OLDEST seq loaded so far — the
 * first element of the last raw page. Deriving it from a reversed display array
 * instead once produced a cursor NEWER than what was already on screen, which
 * made "load more" re-fetch pages the reader had already seen.
 *
 * `seq` is global across threads, so it is only ever a cursor here: gaps between
 * consecutive seq values within one thread are normal and mean nothing.
 */
export function nextCursor<T extends { seq: number }>(
  newestPage: readonly T[],
  olderPages: readonly (readonly T[])[],
): number | undefined {
  return olderPages.at(-1)?.[0]?.seq ?? newestPage[0]?.seq;
}

/**
 * Union of every page seen so far, ascending by `seq`, de-duplicated by `id`.
 *
 * Concatenating [older pages, newest page] instead drops events. The newest
 * page is refetched whenever a live event arrives, so it slides forward; the
 * older pages were fetched with an exclusive `before` captured at an earlier
 * position and never move. Everything appended between the two is then in no
 * page at all, and `seq` being global means the gap is undetectable by
 * comparing adjacent values.
 *
 * Retaining an event that a later page no longer returns is correct rather
 * than stale: the log is append-only, so it slid out of the window — it was
 * not revised or removed.
 */
export function mergeById<T extends { id: string; seq: number }>(
  ...pages: readonly (readonly T[])[]
): T[] {
  const byId = new Map<string, T>();
  for (const page of pages) {
    for (const event of page) byId.set(event.id, event);
  }
  return [...byId.values()].sort((a, b) => a.seq - b.seq);
}
