import { loadSeen, saveSeen } from "./board.js";

/**
 * The last log seq the person has seen per work item, shared by the
 * conversation (which advances it) and the sidebar's in-progress list (which
 * marks what is new), and persisted across reloads.
 */
export const seenState = $state<{ map: Record<string, number> }>({ map: loadSeen() });

export function updateSeen(next: Record<string, number>): void {
  seenState.map = next;
  saveSeen(next);
}
