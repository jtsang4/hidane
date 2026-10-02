import { beforeEach, describe, expect, it } from "vitest";
import {
  canonical,
  inSettings,
  leaveSettings,
  navigate,
  openSettings,
  routeFor,
  routerState,
  toggleSettings,
} from "../src/lib/router.svelte.js";

beforeEach(() => {
  navigate("/");
});

describe("settings as its own surface", () => {
  it("returns to the page it was opened from, query and all", () => {
    navigate("/?focus=wi_a");
    openSettings();
    expect(routerState.path).toBe("/settings/general");
    expect(window.location.pathname).toBe("/settings/general");
    navigate("/settings/roles");
    navigate("/settings/rules");
    leaveSettings();
    expect(routerState.path).toBe("/");
    expect(routerState.search).toBe("?focus=wi_a");
    expect(window.location.search).toBe("?focus=wi_a");
  });

  it("goes back to where it came from, not one step back in history", () => {
    navigate("/memory");
    navigate("/schedules");
    openSettings("providers");
    expect(routeFor(routerState.path)).toEqual({ name: "settings", section: "providers" });
    leaveSettings();
    expect(routerState.path).toBe("/schedules");
  });

  it("toggles like ⌘,", () => {
    navigate("/items");
    toggleSettings();
    expect(inSettings()).toBe(true);
    // Already open at a section: opening again keeps it.
    navigate("/settings/status");
    openSettings();
    expect(routerState.path).toBe("/settings/status");
    toggleSettings();
    expect(routerState.path).toBe("/items");
  });

  it("falls back to the conversation when settings was the first page loaded", () => {
    routerState.returnTo = "/";
    navigate("/settings/about");
    leaveSettings();
    expect(routerState.path).toBe("/");
  });
});

describe("addresses that moved", () => {
  it("lands old pages on their settings sections and work items beside the conversation", () => {
    expect(canonical("/policies", "")).toEqual({ path: "/settings/rules", search: "" });
    expect(canonical("/status", "?x=1")).toEqual({ path: "/settings/status", search: "" });
    expect(canonical("/settings", "")).toEqual({ path: "/settings/general", search: "" });
    expect(canonical("/items/wi_a", "")).toEqual({ path: "/", search: "?focus=wi_a" });
    expect(canonical("/memory", "")).toEqual({ path: "/memory", search: "" });
  });

  it("navigating to an old address replaces it", () => {
    navigate("/events");
    expect(routerState.path).toBe("/settings/events");
    expect(window.location.pathname).toBe("/settings/events");
  });
});
