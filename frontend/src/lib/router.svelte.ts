export const SETTINGS_SECTIONS = [
  "general",
  "shortcuts",
  "about",
  "roles",
  "providers",
  "cli",
  "rules",
  "status",
  "events",
] as const;
export type SettingsSection = (typeof SETTINGS_SECTIONS)[number];

export const SETTINGS_GROUPS: readonly { id: "app" | "agents" | "diagnostics"; sections: readonly SettingsSection[] }[] = [
  { id: "app", sections: ["general", "shortcuts", "about"] },
  { id: "agents", sections: ["roles", "providers", "cli", "rules"] },
  { id: "diagnostics", sections: ["status", "events"] },
];

export function isSettingsSection(value: string): value is SettingsSection {
  return (SETTINGS_SECTIONS as readonly string[]).includes(value);
}

export type Route =
  | { name: "chat" }
  | { name: "items" }
  | { name: "item"; id: string }
  | { name: "log" }
  | { name: "memory" }
  | { name: "schedules" }
  | { name: "settings"; section: SettingsSection }
  /** An old address whose page moved; replaced in history, never shown. */
  | { name: "redirect"; to: string }
  | { name: "not-found" };

/** Pages that moved into settings, so old links and bookmarks still land. */
const MOVED: Readonly<Record<string, string>> = {
  "/settings": "/settings/general",
  "/policies": "/settings/rules",
  "/status": "/settings/status",
  "/events": "/settings/events",
};

function normalize(path: string): string {
  if (!path || path === "/") return "/";
  const trimmed = path.replace(/\/+$/, "");
  return trimmed || "/";
}

export function routeFor(path: string): Route {
  const normalized = normalize(path);
  if (normalized === "/") return { name: "chat" };
  if (normalized === "/items") return { name: "items" };
  if (normalized.startsWith("/items/")) {
    const id = normalized.slice("/items/".length);
    return id ? { name: "item", id: decodeURIComponent(id) } : { name: "not-found" };
  }
  if (normalized === "/log") return { name: "log" };
  if (normalized === "/memory") return { name: "memory" };
  if (normalized === "/schedules") return { name: "schedules" };
  const moved = MOVED[normalized];
  if (moved) return { name: "redirect", to: moved };
  if (normalized.startsWith("/settings/")) {
    const section = normalized.slice("/settings/".length);
    return isSettingsSection(section) ? { name: "settings", section } : { name: "redirect", to: MOVED["/settings"]! };
  }
  return { name: "not-found" };
}

/** A page of the main window — where leaving settings goes back to. */
function isMainPath(path: string): boolean {
  const name = routeFor(path).name;
  return name === "chat" || name === "items" || name === "item" || name === "log" || name === "memory" || name === "schedules";
}

/**
 * Where an address really lives: pages that moved into settings, and the old
 * work item page, which now opens beside the conversation.
 */
export function canonical(path: string, search: string): { path: string; search: string } {
  const route = routeFor(path);
  if (route.name === "redirect") return { path: route.to, search: "" };
  if (route.name === "item") {
    const url = new URL(focusHref(route.id), "http://local");
    return { path: url.pathname, search: url.search };
  }
  return { path, search };
}

function initial(): { path: string; search: string } {
  if (typeof window === "undefined") return { path: "/", search: "" };
  const start = canonical(normalize(window.location.pathname), window.location.search);
  if (start.path + start.search !== window.location.pathname + window.location.search) {
    window.history.replaceState({}, "", start.path + start.search);
  }
  return start;
}

const start = initial();
const initialPath = start.path;
const initialSearch = start.search;

export const routerState = $state({
  path: initialPath,
  search: initialSearch,
  /** The last main-window location, so settings closes to where it was opened from. */
  returnTo: isMainPath(initialPath) ? initialPath + initialSearch : "/",
});

function settle(path: string, search: string): void {
  routerState.path = path;
  routerState.search = search;
  if (isMainPath(path)) routerState.returnTo = path + search;
}

/** The work item open in the focus panel, from `?focus=`. */
export function focusFrom(search = routerState.search): string | null {
  const id = new URLSearchParams(search).get("focus");
  return id && id.trim() ? id : null;
}

/** Link to the conversation with a work item open beside it. */
export function focusHref(id: string | null): string {
  return conversationHref({ focus: id });
}

/** The event the conversation is opened at, from `?at=` — a permalink. */
export function atFrom(search = routerState.search): string | null {
  const id = new URLSearchParams(search).get("at");
  return id && id.trim() ? id : null;
}

/** Link to the conversation, optionally opened at an event and/or with a work item beside it. */
export function conversationHref(opts: { at?: string | null; focus?: string | null }): string {
  const q = new URLSearchParams();
  if (opts.focus) q.set("focus", opts.focus);
  if (opts.at) q.set("at", opts.at);
  const search = q.toString();
  return search ? `/?${search}` : "/";
}

export function navigate(target: string, opts: { replace?: boolean } = {}): void {
  const url = new URL(target, "http://local");
  const { path, search } = canonical(normalize(url.pathname), url.search);
  if (typeof window !== "undefined" && window.location.pathname + window.location.search !== path + search) {
    if (opts.replace) window.history.replaceState({}, "", path + search);
    else window.history.pushState({}, "", path + search);
  }
  settle(path, search);
}

export function inSettings(path = routerState.path): boolean {
  return routeFor(path).name === "settings";
}

export function settingsHref(section: SettingsSection): string {
  return `/settings/${section}`;
}

/** Open settings at a section; with none, keep the section already shown or start at General. */
export function openSettings(section?: SettingsSection): void {
  if (!section && inSettings()) return;
  navigate(settingsHref(section ?? "general"));
}

/** Back to the main-window page settings was opened from — not browser history. */
export function leaveSettings(): void {
  if (!inSettings()) return;
  navigate(routerState.returnTo || "/");
}

export function toggleSettings(): void {
  if (inSettings()) leaveSettings();
  else openSettings();
}

export function initRouter(): () => void {
  if (typeof window === "undefined") return () => undefined;
  const onPopState = () => {
    const at = canonical(normalize(window.location.pathname), window.location.search);
    if (at.path + at.search !== window.location.pathname + window.location.search) {
      window.history.replaceState({}, "", at.path + at.search);
    }
    settle(at.path, at.search);
  };
  window.addEventListener("popstate", onPopState);
  return () => window.removeEventListener("popstate", onPopState);
}
