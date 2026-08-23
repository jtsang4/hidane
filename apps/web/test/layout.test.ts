import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const read = (p: string) => readFileSync(join(process.cwd(), p), "utf8");

/**
 * Layout regressions are invisible to logic tests and only show up on a phone,
 * so the few classes that carry a real constraint are asserted directly.
 */
describe("mobile layout invariants", () => {
  it("buttons never break their label across lines", () => {
    // CJK breaks between any two characters: a squeezed button rendered "新建"
    // as one glyph per line on a phone.
    expect(read("src/components/ui/primitives.tsx")).toContain("whitespace-nowrap");
  });

  it("bottom nav items flex to the viewport instead of a fixed row width", () => {
    // Seven links plus three controls measured 400px wide on a 390px phone,
    // scrolling the whole app sideways.
    const main = read("src/main.tsx");
    const navItems = main.match(/min-w-0 flex-1/g) ?? [];
    expect(navItems.length).toBeGreaterThanOrEqual(3);
    expect(main).toContain("env(safe-area-inset-bottom)");
  });

  it("toasts clear the bottom nav on mobile", () => {
    // An error toast used to cover the nav — exactly when you want to leave.
    expect(read("src/components/Toaster.tsx")).toContain("bottom-24");
  });

  it("worklog code spans wrap instead of widening the page", () => {
    expect(read("src/pages/LogPage.tsx")).toContain("[&_code]:break-all");
  });
});
