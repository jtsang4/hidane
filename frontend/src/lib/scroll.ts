/** The parts of a scroll container this module needs, so it can be tested without a DOM. */
export interface ScrollMetrics {
  offsetHeight: number;
  scrollTop: number;
  scrollHeight: number;
}

/** How far from the bottom still counts as "following the conversation". */
export const PINNED_SLACK_PX = 50;

/**
 * Is the reader sitting at the live edge of a scroll container?
 *
 * Must be measured *before* the DOM updates: once new bubbles are inserted,
 * "pinned to the bottom" and "scrolled up reading history" look identical, and
 * autoscrolling on the latter yanks the reader away from what they were reading.
 *
 * A viewport shorter than its content counts as pinned, which is what lands a
 * freshly loaded conversation on its newest message instead of its first.
 */
export function isPinnedToBottom(
  el: ScrollMetrics,
  slackPx = PINNED_SLACK_PX,
): boolean {
  return el.offsetHeight + el.scrollTop > el.scrollHeight - slackPx;
}
