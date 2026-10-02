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
    expect(read("src/components/ui/Button.svelte")).toContain("whitespace-nowrap");
  });

  it("bottom nav items flex to the viewport instead of a fixed row width", () => {
    // Seven links plus three controls measured 400px wide on a 390px phone,
    // scrolling the whole app sideways.
    const nav = read("src/components/PhoneNav.svelte");
    expect(nav).toContain("min-w-0 flex-1");
    expect(nav).toContain("env(safe-area-inset-bottom)");
    expect(nav).toContain("sm:hidden");
  });

  it("toasts stay clear of the bottom nav and the composer", () => {
    // An error toast used to cover the nav — exactly when you want to leave.
    const toaster = read("src/components/Toaster.svelte");
    expect(toaster).toContain("top-14");
    expect(toaster).not.toMatch(/\bbottom-\d/);
  });

  it("the window never scrolls as a page: only content regions do", () => {
    const css = read("src/styles.css");
    expect(css).toMatch(/overflow: hidden;\s*overscroll-behavior: none;/);
    expect(css).toContain("--wails-draggable: drag");
    expect(css).toContain("--wails-draggable: no-drag");
  });

  it("worklog code spans wrap instead of widening the page", () => {
    expect(read("src/pages/LogPage.svelte")).toContain("[&_code]:break-all");
  });
});
