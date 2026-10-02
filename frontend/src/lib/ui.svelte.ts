/**
 * Window-level UI state shared by the shell, the overlays and the pages, plus
 * the few preferences that live only on this machine (localStorage).
 */

const SIDEBAR_KEY = "hidane-sidebar-collapsed";
const NOTIFY_KEY = "hidane-notify";
const BADGE_KEY = "hidane-badge";

function readFlag(key: string, fallback: boolean): boolean {
  if (typeof localStorage === "undefined") return fallback;
  const value = localStorage.getItem(key);
  return value === null ? fallback : value === "1";
}

function writeFlag(key: string, value: boolean): void {
  if (typeof localStorage !== "undefined") localStorage.setItem(key, value ? "1" : "0");
}

export const ui = $state({
  paletteOpen: false,
  newTaskOpen: false,
  sidebarCollapsed: readFlag(SIDEBAR_KEY, false),
  /** Set to ask the conversation to focus its composer once it is shown; it clears it. */
  composerFocus: false,
});

export const prefs = $state({
  /** Announce finished work while the window is in the background. */
  notify: readFlag(NOTIFY_KEY, true),
  /** Unread count on the Dock icon (desktop) or in the tab title (browser). */
  badge: readFlag(BADGE_KEY, true),
});

export function setSidebarCollapsed(collapsed: boolean): void {
  ui.sidebarCollapsed = collapsed;
  writeFlag(SIDEBAR_KEY, collapsed);
}

export function setPref(key: "notify" | "badge", value: boolean): void {
  prefs[key] = value;
  writeFlag(key === "notify" ? NOTIFY_KEY : BADGE_KEY, value);
}

/** Re-read persisted state; tests reset localStorage between cases. */
export function reloadUiState(): void {
  ui.sidebarCollapsed = readFlag(SIDEBAR_KEY, false);
  prefs.notify = readFlag(NOTIFY_KEY, true);
  prefs.badge = readFlag(BADGE_KEY, true);
}
