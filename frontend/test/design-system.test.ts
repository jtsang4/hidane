import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The design system (DESIGN.md) lives in `src/styles.css` (`@theme`) and the
 * components built on it. Tailwind's own palette, shadows and radii are
 * switched off there, so an off-system class such as `bg-orange-500` or
 * `shadow-xl` would not fail the build: it would silently render nothing.
 * These rules catch such a class, and the one-off values that bypass the
 * tokens altogether, before they reach a screen.
 */

const root = join(process.cwd(), "src");

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sources(path);
    return /\.(svelte|ts)$/.test(entry.name) ? [path] : [];
  });
}

const PALETTE = "slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|white|black";
const COLOR_UTILITY = "bg|text|border(?:-[xytrblse])?|ring(?:-offset)?|outline|from|via|to|fill|stroke|divide|decoration|shadow|inset-shadow|drop-shadow|placeholder|caret|accent";

const rules: { name: string; why: string; pattern: RegExp }[] = [
  {
    name: "color literal",
    why: "colors come from the --color-* tokens in styles.css",
    pattern: /(?<![{\w])#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|color-mix)\(/,
  },
  {
    name: "Tailwind palette color",
    why: "the default palette is switched off; use a token such as bg-accent, bg-overlay or text-danger-foreground",
    pattern: new RegExp(`(?<![\\w-])(?:${COLOR_UTILITY})-(?:${PALETTE})(?:-\\d{2,3})?(?![\\w-])`),
  },
  {
    name: "Tailwind shadow step",
    why: "only the theme's shadows exist (shadow-popover, shadow-dialog, shadow-primary, …)",
    pattern: /(?<![\w-])(?:shadow|inset-shadow|drop-shadow)-(?:2xs|xs|sm|md|lg|xl|2xl)(?![\w-])/,
  },
  {
    name: "radius outside the scale",
    why: "radii are rounded-sm / md / lg / xl / 2xl / full; a bare `rounded` sits outside the theme",
    pattern: /(?<![\w-])rounded(?:-[trblse]{1,2})?(?:-(?:xs|3xl|4xl))?(?![\w-])/,
  },
  {
    name: "one-off color, shadow, radius or text size",
    why: "add a token to @theme instead of an arbitrary value",
    pattern: /(?<![\w-])(?:bg|text|from|via|to|fill|stroke|ring|divide|shadow|inset-shadow|drop-shadow|rounded(?:-[trblse]{1,2})?)-\[|(?<![\w-])border(?:-[xytrblse])?-\[(?!\d)|\[(?:box-shadow|background|background-color|color|border-color|border-radius|font-size):/,
  },
];

function violations(): string[] {
  const found: string[] = [];
  for (const file of sources(root)) {
    const lines = readFileSync(file, "utf8").split("\n");
    lines.forEach((line, index) => {
      for (const rule of rules) {
        const match = rule.pattern.exec(line);
        if (match) found.push(`${relative(process.cwd(), file)}:${index + 1}: ${rule.name} "${match[0]}" (${rule.why})`);
      }
    });
  }
  return found;
}

describe("design system", () => {
  it("components draw colors, shadows, radii and type sizes only from the tokens", () => {
    expect(violations()).toEqual([]);
  });

  it("Tailwind's default palette, shadows and radii are switched off in the theme", () => {
    const css = readFileSync(join(root, "styles.css"), "utf8");
    expect(css).toContain("--color-*: initial;");
    expect(css).toContain("--shadow-*: initial;");
    expect(css).toContain("--radius-*: initial;");
  });

  it("each rule catches what it is meant to", () => {
    const hits = (text: string) => rules.filter((rule) => rule.pattern.test(text)).map((rule) => rule.name);
    expect(hits('class="bg-orange-500"')).toEqual(["Tailwind palette color"]);
    expect(hits('class="hover:bg-white/10"')).toEqual(["Tailwind palette color"]);
    expect(hits('class="shadow-xl"')).toEqual(["Tailwind shadow step"]);
    expect(hits('class="rounded px-1"')).toEqual(["radius outside the scale"]);
    expect(hits('class="rounded-3xl"')).toEqual(["radius outside the scale"]);
    expect(hits('class="text-[13px]"')).toEqual(["one-off color, shadow, radius or text size"]);
    expect(hits('class="shadow-[0_1px_2px_black]"')).toEqual(["one-off color, shadow, radius or text size"]);
    expect(hits('style="color: #ef852e"')).toEqual(["color literal"]);
    // What the system itself uses passes.
    expect(hits('class="rounded-md bg-accent text-muted shadow-popover border-[1.5px] border-primary/60 rounded-t-lg rounded-full"')).toEqual([]);
    expect(hits("{#each items as item}{#if open}")).toEqual([]);
  });
});
