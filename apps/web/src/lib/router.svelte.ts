export type Route =
  | { name: "chat" }
  | { name: "items" }
  | { name: "item"; id: string }
  | { name: "events" }
  | { name: "log" }
  | { name: "memory" }
  | { name: "schedules" }
  | { name: "status" }
  | { name: "not-found" };

function normalize(path: string): string {
  if (!path || path === "/") return "/";
  const trimmed = path.replace(/\/+$/, "");
  return trimmed || "/";
}

export const routerState = $state({
  path: typeof window === "undefined" ? "/" : normalize(window.location.pathname),
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
  if (normalized === "/status") return { name: "status" };
  return { name: "not-found" };
}

export function navigate(path: string): void {
  const normalized = normalize(path);
  if (typeof window !== "undefined" && window.location.pathname !== normalized) {
    window.history.pushState({}, "", normalized);
  }
  routerState.path = normalized;
}

export function initRouter(): () => void {
  if (typeof window === "undefined") return () => undefined;
  const onPopState = () => {
    routerState.path = normalize(window.location.pathname);
  };
  window.addEventListener("popstate", onPopState);
  return () => window.removeEventListener("popstate", onPopState);
}
