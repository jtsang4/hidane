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
