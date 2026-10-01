import { afterEach, describe, expect, it } from "vitest";
import { DEFAULT_BOOT, boot, parseBoot } from "../src/lib/boot.js";

afterEach(() => {
  delete window.hidaneBoot;
});

describe("boot info", () => {
  it("defaults to the browser shape with auth when /boot.js did not run", () => {
    expect(boot()).toEqual({ desktop: false, auth: true, version: "dev" });
    expect(DEFAULT_BOOT).toEqual({ desktop: false, auth: true, version: "dev" });
  });

  it("reads what the host published", () => {
    window.hidaneBoot = { desktop: true, auth: false, version: "1.2.3" };
    expect(boot()).toEqual({ desktop: true, auth: false, version: "1.2.3" });
  });

  it("never disables auth because a field is missing or mistyped", () => {
    expect(parseBoot({ desktop: true })).toEqual({ desktop: true, auth: true, version: "dev" });
    expect(parseBoot({ auth: "false", desktop: 1, version: 7 })).toEqual(DEFAULT_BOOT);
    expect(parseBoot(null)).toEqual(DEFAULT_BOOT);
    expect(parseBoot("desktop")).toEqual(DEFAULT_BOOT);
  });

  it("hands out copies, so callers cannot mutate the defaults", () => {
    const info = boot();
    info.auth = false;
    expect(boot().auth).toBe(true);
  });
});
