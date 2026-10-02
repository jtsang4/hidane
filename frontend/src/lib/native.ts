import { api } from "./api.js";
import { boot } from "./boot.js";

/**
 * What the desktop host does for the page, with the browser's nearest
 * equivalent. Every host endpoint is 404 in serve mode and may fail in an
 * unpackaged app, so callers that only decorate (notify, badge) never throw.
 */

/** Desktop: the host clipboard (WKWebView's `navigator.clipboard` is unreliable on a custom scheme). */
export async function copyText(text: string, desktop = boot().desktop): Promise<void> {
  if (desktop) {
    try {
      await api.desktopClipboard(text);
      return;
    } catch {
      // Fall through: a browser pretending to be the app still has a clipboard.
    }
  }
  if (typeof navigator === "undefined" || !navigator.clipboard) throw new Error("clipboard unavailable");
  await navigator.clipboard.writeText(text);
}

export function nativeNotify(title: string, body: string): void {
  void api.desktopNotify(title, body).catch(() => undefined);
}

export function nativeBadge(count: number): void {
  void api.desktopBadge(Math.max(0, count)).catch(() => undefined);
}

/** The directory holding a file, for either separator. */
export function parentDir(path: string): string {
  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return cut > 0 ? path.slice(0, cut) : path;
}
