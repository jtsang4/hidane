import { beforeEach, describe, expect, it } from "vitest";
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
