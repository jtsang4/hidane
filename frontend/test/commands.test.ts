import { afterEach, describe, expect, it, vi } from "vitest";
import {
  WAILS_COMMAND_EVENT,
  commandForKey,
  commandFromWails,
  createCommandRunner,
  formatShortcut,
  installCommands,
  type Command,
} from "../src/lib/commands.js";

const key = (k: string, mods: Partial<{ metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean; repeat: boolean }> = {}) => ({
  key: k,
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  ...mods,
});

function fakeRuntime() {
  const callbacks = new Map<string, (event: unknown) => void>();
  const off = vi.fn();
  const module = {
    Events: {
      On: vi.fn((name: string, callback: (event: unknown) => void) => {
        callbacks.set(name, callback);
        return () => {
          callbacks.delete(name);
          off(name);
        };
      }),
    },
  };
  return { module, off, fire: (event: unknown) => callbacks.get(WAILS_COMMAND_EVENT)?.(event) };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("shortcuts", () => {
  it("uses ⌘ on macOS and Ctrl elsewhere, for exactly the menu's keys", () => {
    expect(commandForKey(key(",", { metaKey: true }), true)).toBe("open-settings");
    expect(commandForKey(key("N", { metaKey: true }), true)).toBe("new-task");
    expect(commandForKey(key("l", { metaKey: true }), true)).toBe("focus-composer");
    expect(["1", "2", "3", "4", "5"].map((k) => commandForKey(key(k, { metaKey: true }), true))).toEqual(["go:chat", "go:items", "go:schedules", "go:memory", "go:log"]);
    expect(commandForKey(key("k", { metaKey: true }), true)).toBe("search");
    expect(commandForKey(key("b", { metaKey: true }), true)).toBe("toggle-sidebar");
    expect(commandForKey(key("k", { ctrlKey: true }), false)).toBe("search");
    // The other platform's modifier, extra modifiers, plain keys and auto-repeat do nothing.
    expect(commandForKey(key("k", { ctrlKey: true }), true)).toBeNull();
    expect(commandForKey(key("k", { metaKey: true }), false)).toBeNull();
    expect(commandForKey(key("k", { metaKey: true, shiftKey: true }), true)).toBeNull();
    expect(commandForKey(key("k"), true)).toBeNull();
    expect(commandForKey(key("b", { metaKey: true, repeat: true }), true)).toBeNull();
    expect(commandForKey(key("x", { metaKey: true }), true)).toBeNull();
  });

  it("formats keys for the platform", () => {
    expect(formatShortcut(",", true)).toBe("⌘,");
    expect(formatShortcut("k", true)).toBe("⌘K");
    expect(formatShortcut("k", false)).toBe("Ctrl+K");
  });
});

describe("command runner", () => {
  it("drops the menu's echo of a key press (either order) within 200ms", () => {
    let now = 1_000;
    const ran: Command[] = [];
    const run = createCommandRunner((command) => ran.push(command), { now: () => now });
    expect(run("toggle-sidebar", "menu")).toBe(true);
    now += 50;
    expect(run("toggle-sidebar", "key")).toBe(false);
    // A different command is not a duplicate.
    expect(run("search", "key")).toBe(true);
    now += 200;
    expect(run("toggle-sidebar", "key")).toBe(true);
    now += 30;
    expect(run("toggle-sidebar", "menu")).toBe(false);
    expect(ran).toEqual(["toggle-sidebar", "search", "toggle-sidebar"]);
  });

  it("keeps two quick presses on the same source: they are two presses", () => {
    let now = 1_000;
    const ran: Command[] = [];
    const run = createCommandRunner((command) => ran.push(command), { now: () => now });
    expect(run("open-settings", "key")).toBe(true);
    now += 40;
    expect(run("open-settings", "key")).toBe(true);
    expect(ran).toEqual(["open-settings", "open-settings"]);
  });
});

describe("Wails menu commands", () => {
  it("unwraps every shape the runtime delivers", () => {
    expect(commandFromWails({ name: WAILS_COMMAND_EVENT, data: { command: "search" } })).toBe("search");
    expect(commandFromWails({ name: WAILS_COMMAND_EVENT, data: [{ command: "go:log" }] })).toBe("go:log");
    expect(commandFromWails({ command: "new-task" })).toBe("new-task");
    expect(commandFromWails({ data: JSON.stringify({ command: "open-settings" }) })).toBe("open-settings");
    expect(commandFromWails({ data: { command: "rm -rf" } })).toBeNull();
    expect(commandFromWails(null)).toBeNull();
  });
});

describe("installCommands", () => {
  const cleanups: Array<() => void> = [];
  afterEach(() => {
    while (cleanups.length > 0) cleanups.pop()?.();
  });

  it("delivers menu events and page shortcuts, and a key press arriving both ways runs once", async () => {
    const runtime = fakeRuntime();
    const ran: Command[] = [];
    const run = createCommandRunner((command) => ran.push(command));
    cleanups.push(installCommands(run, { desktop: true, mac: true, importRuntime: () => Promise.resolve(runtime.module) }));
    await flush();
    expect(runtime.module.Events.On).toHaveBeenCalledWith(WAILS_COMMAND_EVENT, expect.any(Function));

    // ⌘B: the menu's key equivalent and the page keydown both fire.
    runtime.fire({ name: WAILS_COMMAND_EVENT, data: [{ command: "toggle-sidebar" }] });
    const event = new KeyboardEvent("keydown", { key: "b", metaKey: true, cancelable: true });
    window.dispatchEvent(event);
    expect(ran).toEqual(["toggle-sidebar"]);
    expect(event.defaultPrevented).toBe(true);

    runtime.fire({ data: { command: "go:memory" } });
    expect(ran).toEqual(["toggle-sidebar", "go:memory"]);
  });

  it("works without the desktop runtime and stops listening when removed", async () => {
    const ran: Command[] = [];
    const stop = installCommands((command) => ran.push(command), { desktop: true, mac: false, importRuntime: () => Promise.reject(new Error("404")) });
    await flush();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true }));
    expect(ran).toEqual(["search"]);
    stop();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true }));
    expect(ran).toEqual(["search"]);
  });

  it("removes the menu subscription on close", async () => {
    const runtime = fakeRuntime();
    const stop = installCommands(() => undefined, { desktop: true, mac: true, importRuntime: () => Promise.resolve(runtime.module) });
    await flush();
    stop();
    expect(runtime.off).toHaveBeenCalledWith(WAILS_COMMAND_EVENT);
  });

  it("never asks for the runtime in the browser", () => {
    const importRuntime = vi.fn(() => Promise.resolve({}));
    cleanups.push(installCommands(() => undefined, { desktop: false, mac: true, importRuntime }));
    expect(importRuntime).not.toHaveBeenCalled();
  });
});
