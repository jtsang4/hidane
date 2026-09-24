export type Route =
  | { name: "chat" }
  | { name: "items" }
  | { name: "item"; id: string }
  | { name: "events" }
  | { name: "log" }
  | { name: "memory" }
  | { name: "schedules" }
  | { name: "policies" }
  | { name: "status" }
  | { name: "not-found" };

function normalize(path: string): string {
  if (!path || path === "/") return "/";
  const trimmed = path.replace(/\/+$/, "");
  return trimmed || "/";
}

export const routerState = $state({
  path: typeof window === "undefined" ? "/" : normalize(window.location.pathname),
  search: typeof window === "undefined" ? "" : window.location.search,
});

export function routeFor(path = routerState.path): Route {
  const normalized = normalize(path);
  if (normalized === "/") return { name: "chat" };
  if (normalized === "/items") return { name: "items" };
  if (normalized.startsWith("/items/")) {
    const id = normalized.slice("/items/".length);
    return id ? { name: "item", id: decodeURIComponent(id) } : { name: "not-found" };
  }
  if (normalized === "/events") return { name: "events" };
  if (normalized === "/log") return { name: "log" };
  if (normalized === "/memory") return { name: "memory" };
  if (normalized === "/schedules") return { name: "schedules" };
  if (normalized === "/policies") return { name: "policies" };
  if (normalized === "/status") return { name: "status" };
  return { name: "not-found" };
}

/** The work item open in the focus panel, from `?focus=`. */
export function focusFrom(search = routerState.search): string | null {
  const id = new URLSearchParams(search).get("focus");
  return id && id.trim() ? id : null;
}

/** Link to the conversation with a work item open beside it. */
export function focusHref(id: string | null): string {
  return id ? `/?focus=${encodeURIComponent(id)}` : "/";
}

export function navigate(target: string, opts: { replace?: boolean } = {}): void {
  const url = new URL(target, "http://local");
  const path = normalize(url.pathname);
  const search = url.search;
  if (typeof window !== "undefined" && window.location.pathname + window.location.search !== path + search) {
    if (opts.replace) window.history.replaceState({}, "", path + search);
    else window.history.pushState({}, "", path + search);
  }
  routerState.path = path;
  routerState.search = search;
}

export function initRouter(): () => void {
  if (typeof window === "undefined") return () => undefined;
  const onPopState = () => {
    routerState.path = normalize(window.location.pathname);
    routerState.search = window.location.search;
  };
  window.addEventListener("popstate", onPopState);
  return () => window.removeEventListener("popstate", onPopState);
}
