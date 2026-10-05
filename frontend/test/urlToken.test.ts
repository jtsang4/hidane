import { beforeEach, describe, expect, it, vi } from "vitest";
import { getToken } from "../src/lib/api.js";
import { consumeUrlToken } from "../src/lib/urlToken.js";

describe("consumeUrlToken", () => {
  beforeEach(() => localStorage.clear());

  it("stores the token and strips only it from the address", () => {
    let replaced = "";
    const took = consumeUrlToken({ pathname: "/settings", search: "?focus=wi_1&token=abc", hash: "#x" }, (url) => (replaced = url));
    expect(took).toBe(true);
    expect(getToken()).toBe("abc");
    expect(replaced).toBe("/settings?focus=wi_1#x");
  });

  it("leaves the address alone when there is no token", () => {
    let replaced: string | null = null;
    expect(consumeUrlToken({ pathname: "/", search: "?at=ev_1", hash: "" }, (url) => (replaced = url))).toBe(false);
    expect(replaced).toBeNull();
    expect(getToken()).toBe("");
  });
});

describe("the token in the address the app opens at", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.resetModules();
  });

  /** Loads the router as the app's first paint does: it reads the address once, at import. */
  async function open(url: string) {
    window.history.replaceState(null, "", url);
    return import("../src/lib/router.svelte.js");
  }

  it("is taken before an old address is redirected, which would drop it", async () => {
    const { routerState } = await open("/events?token=abc");
    expect(getToken()).toBe("abc");
    expect(routerState.path).toBe("/settings/events");
    expect(window.location.pathname + window.location.search).toBe("/settings/events");
  });

  it("never reaches the router's own copy of the query, so leaving settings cannot put it back", async () => {
    const { routerState, openSettings, leaveSettings } = await open("/?focus=wi_1&token=abc");
    expect(getToken()).toBe("abc");
    expect(routerState.search).toBe("?focus=wi_1");
    openSettings();
    leaveSettings();
    expect(window.location.search).toBe("?focus=wi_1");
  });
});
