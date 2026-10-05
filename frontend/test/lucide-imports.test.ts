import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * `import { X } from "@lucide/svelte"` makes every test that reaches the file
 * compile the whole icon set (about 1,900 Svelte components), which once cost
 * the unit tests more than half their time. Icons come one per module;
 * App.svelte alone takes `setLucideProps` from the barrel, which exports
 * nothing else of it.
 */

const root = join(process.cwd(), "src");

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sources(path);
    return /\.(svelte|ts)$/.test(entry.name) ? [path] : [];
  });
}

describe("lucide imports", () => {
  it("import each icon from its own module", () => {
    const offenders: string[] = [];
    for (const file of sources(root)) {
      for (const match of readFileSync(file, "utf8").matchAll(/import\s+(type\s+)?\{([^}]*)\}\s+from\s+"@lucide\/svelte"/g)) {
        if (match[1]) continue;
        const names = (match[2] ?? "").split(",").map((name) => name.trim()).filter(Boolean);
        const icons = names.filter((name) => name !== "setLucideProps");
        if (icons.length > 0 || relative(root, file) !== "App.svelte") offenders.push(`${relative(root, file)}: ${names.join(", ")}`);
      }
    }
    expect(offenders, 'use import X from "@lucide/svelte/icons/<name>"').toEqual([]);
  });
});
