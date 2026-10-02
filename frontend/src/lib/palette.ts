/**
 * The command palette's pure parts: which commands match a query, and how the
 * selection moves through a list that wraps around.
 */

export interface Searchable {
  label: string;
  /** Other words that should find it — the other language's label, a path. */
  keywords?: readonly string[];
}

function rank(entry: Searchable, terms: readonly string[], whole: string): number | null {
  const label = entry.label.toLowerCase();
  const haystack = [label, ...(entry.keywords ?? []).map((k) => k.toLowerCase())].join(" ");
  if (!terms.every((term) => haystack.includes(term))) return null;
  if (label.startsWith(whole)) return 0;
  if (label.includes(whole)) return 1;
  return terms.every((term) => label.includes(term)) ? 2 : 3;
}

/** Every whitespace-separated term must match; label prefixes rank first, original order breaks ties. */
export function filterCommands<T extends Searchable>(entries: readonly T[], query: string): T[] {
  const whole = query.trim().toLowerCase();
  if (!whole) return [...entries];
  const terms = whole.split(/\s+/);
  return entries
    .map((entry, index) => ({ entry, index, score: rank(entry, terms, whole) }))
    .filter((candidate): candidate is { entry: T; index: number; score: number } => candidate.score !== null)
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .map((candidate) => candidate.entry);
}

/** The next selected index after moving `delta`, wrapping at both ends; -1 for an empty list. */
export function stepSelection(index: number, delta: number, count: number): number {
  if (count <= 0) return -1;
  if (index < 0) return delta < 0 ? count - 1 : 0;
  return (((index + delta) % count) + count) % count;
}
