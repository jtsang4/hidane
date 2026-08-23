import { beforeEach, describe, expect, it } from "vitest";
import i18n from "../src/i18n/index.js";
import { fmtRelative } from "../src/lib/utils.js";

const NOW = Date.UTC(2026, 7, 24, 12, 0, 0);
const ago = (seconds: number) => new Date(NOW - seconds * 1000).toISOString();

beforeEach(async () => {
  await i18n.changeLanguage("zh");
});

describe("fmtRelative", () => {
  it("reads as how-long-ago across the useful range", () => {
    expect(fmtRelative(ago(5), NOW)).toBe("刚刚");
    expect(fmtRelative(ago(44), NOW)).toBe("刚刚");
    expect(fmtRelative(ago(60), NOW)).toBe("1 分钟前");
    expect(fmtRelative(ago(300), NOW)).toBe("5 分钟前");
    expect(fmtRelative(ago(3600), NOW)).toBe("1 小时前");
    expect(fmtRelative(ago(3600 * 5), NOW)).toBe("5 小时前");
    expect(fmtRelative(ago(3600 * 48), NOW)).toBe("2 天前");
  });

  it("says 'yesterday' with a clock time rather than '1 天前'", () => {
    expect(fmtRelative(ago(3600 * 25), NOW)).toContain("昨天");
  });

  it("falls back to an absolute date once relative stops helping", () => {
    // "13 天前" is harder to place than the date itself.
    const old = fmtRelative(ago(3600 * 24 * 13), NOW);
    expect(old).not.toContain("天前");
    expect(old).toMatch(/2026/);
  });

  it("does not render a future time as a negative age", () => {
    // Server and browser clocks disagree; a fresh event must not read "-2m".
    expect(fmtRelative(new Date(NOW + 5000).toISOString(), NOW)).toBe("刚刚");
  });

  it("passes through something that is not a date", () => {
    expect(fmtRelative("not-a-date", NOW)).toBe("not-a-date");
  });

  it("switches language with the rest of the UI", async () => {
    await i18n.changeLanguage("en");
    expect(fmtRelative(ago(300), NOW)).toBe("5m ago");
    expect(fmtRelative(ago(5), NOW)).toBe("just now");
  });
});
