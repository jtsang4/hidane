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

export type FollowDecision = "follow" | "unfollow" | "keep" | "repin";

/**
 * What a scroll event means for following the live edge.
 *
 * Only the reader leaves the bottom: content that grows after the follow
 * (a task card updating, Markdown settling, an image loading, the composer
 * growing) also produces a scroll position short of the end, and treating
 * that as "scrolled away" stranded the view 50–90px above the newest reply
 * with "back to latest" and a new-reply notice covering it. Without a recent
 * wheel, touch, key or pointer on the scroller, a follower is put back.
 */
export function followAfterScroll(state: {
  /** Showing the newest page; a window of history is never followed. */
  live: boolean;
  pinned: boolean;
  /** The reader touched the scroller just now. */
  userInitiated: boolean;
  following: boolean;
}): FollowDecision {
  if (!state.live) return "unfollow";
  if (state.pinned) return "follow";
  if (state.userInitiated) return "unfollow";
  return state.following ? "repin" : "keep";
}
