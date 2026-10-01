/** How the host started the SPA, published by the server's `/boot.js`. */
export interface BootInfo {
  /** Running inside the Wails desktop webview rather than a browser. */
  desktop: boolean;
  /** Whether `/api/*` needs a bearer token. False only when the webview is the sole client. */
  auth: boolean;
  version: string;
}

declare global {
  interface Window {
    hidaneBoot?: unknown;
  }
}

/**
 * Without `/boot.js` (tests, a bare Vite dev server) assume the stricter
 * browser shape: a token gate is safe to show, a missing desktop runtime is not.
 */
export const DEFAULT_BOOT: Readonly<BootInfo> = Object.freeze({ desktop: false, auth: true, version: "dev" });

/** Field-by-field so a partial or malformed `window.hidaneBoot` cannot disable auth by accident. */
export function parseBoot(raw: unknown): BootInfo {
  if (typeof raw !== "object" || raw === null) return { ...DEFAULT_BOOT };
  const value = raw as Record<string, unknown>;
  return {
    desktop: typeof value.desktop === "boolean" ? value.desktop : DEFAULT_BOOT.desktop,
    auth: typeof value.auth === "boolean" ? value.auth : DEFAULT_BOOT.auth,
    version: typeof value.version === "string" && value.version ? value.version : DEFAULT_BOOT.version,
  };
}

export function boot(): BootInfo {
  return parseBoot(typeof window === "undefined" ? undefined : window.hidaneBoot);
}
