import { setToken } from "./api.js";

/**
 * `hidane serve` prints a link carrying `?token=`. The token is taken once at
 * startup and removed from the address, so it is not left in history,
 * bookmarks, or a URL someone copies to share a conversation.
 */
export function consumeUrlToken(
  location: { pathname: string; search: string; hash: string } = window.location,
  replace: (url: string) => void = (url) => window.history.replaceState(window.history.state, "", url),
): boolean {
  const params = new URLSearchParams(location.search);
  const token = params.get("token")?.trim();
  if (!token) return false;
  setToken(token);
  params.delete("token");
  const search = params.toString();
  replace(`${location.pathname}${search ? `?${search}` : ""}${location.hash}`);
  return true;
}
