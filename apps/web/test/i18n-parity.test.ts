import { describe, expect, it } from "vitest";
import { en, zh } from "../src/i18n/resources.js";

/**
 * `AppResources = typeof zh` makes zh the compile-time key space, so a typo in
 * `$t("...")` fails `svelte-check`. `en` carries no such constraint: a key added
 * to zh and forgotten in en type-checks cleanly and only shows up at runtime as
 * an English user silently reading Chinese. This closes that gap.
 */
function paths(node: unknown, prefix = ""): string[] {
  if (typeof node !== "object" || node === null) return [prefix];
  return Object.entries(node).flatMap(([key, value]) =>
    paths(value, prefix ? `${prefix}.${key}` : key),
  );
}

describe("i18n resource parity", () => {
  it("defines the same key set in zh and en", () => {
    const zhKeys = paths(zh).sort();
    const enKeys = paths(en).sort();
    expect(enKeys.filter((key) => !zhKeys.includes(key))).toEqual([]);
    expect(zhKeys.filter((key) => !enKeys.includes(key))).toEqual([]);
  });

  it("leaves no key with an empty string in either locale", () => {
    const empties = (node: unknown, prefix = ""): string[] => {
      if (typeof node === "string") return node.trim() ? [] : [prefix];
      if (typeof node !== "object" || node === null) return [];
      return Object.entries(node).flatMap(([key, value]) =>
        empties(value, prefix ? `${prefix}.${key}` : key),
      );
    };
    expect(empties(zh)).toEqual([]);
    expect(empties(en)).toEqual([]);
  });
});
