import { apiFetch } from "./api.js";

/**
 * In the desktop app the page lives in a webview with no address bar and no
 * back button: following an external link would replace the app itself, and
 * a download link has nowhere to save to. Such clicks are handed to the host
 * instead — the system browser, or the file revealed in the file manager.
 */
export type LinkAction =
  | { kind: "external"; url: string }
  | { kind: "reveal"; workItemId: string; path: string }
  | null;

const ARTIFACT = /^\/api\/work-items\/([^/]+)\/file$/;

/**
 * `base` is the page's full URL. Origins are not compared: for the webview's
 * custom scheme (`wails://`) `URL#origin` is the opaque string "null".
 */
export function linkAction(href: string, base: string): LinkAction {
  let url: URL;
  let page: URL;
  try {
    page = new URL(base);
    url = new URL(href, page);
  } catch {
    return null;
  }
  if (url.protocol !== page.protocol || url.host !== page.host) {
    return url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:"
      ? { kind: "external", url: url.toString() }
      : null;
  }
  const artifact = ARTIFACT.exec(url.pathname);
  if (artifact?.[1] && url.searchParams.has("download")) {
    return { kind: "reveal", workItemId: decodeURIComponent(artifact[1]), path: url.searchParams.get("path") ?? "" };
  }
  return null;
}

async function defaultSend(action: Exclude<LinkAction, null>): Promise<void> {
  if (action.kind === "external") {
    await apiFetch(`/api/desktop/open-url`, { method: "POST", body: JSON.stringify({ url: action.url }) });
  } else {
    await apiFetch(`/api/work-items/${encodeURIComponent(action.workItemId)}/reveal`, {
      method: "POST",
      body: JSON.stringify({ path: action.path }),
    });
  }
}

export function installDesktopLinks(
  doc: Document = document,
  base: () => string = () => window.location.href,
  send: (action: Exclude<LinkAction, null>) => Promise<void> = defaultSend,
): () => void {
  const onClick = (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0) return;
    const target = event.target;
    const anchor = target instanceof Element ? target.closest("a[href]") : null;
    if (!anchor) return;
    const action = linkAction(anchor.getAttribute("href") ?? "", base());
    if (!action) return;
    event.preventDefault();
    void send(action).catch(() => undefined);
  };
  doc.addEventListener("click", onClick, true);
  return () => doc.removeEventListener("click", onClick, true);
}
