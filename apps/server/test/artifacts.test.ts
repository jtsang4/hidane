import { describe, expect, it } from "vitest";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  listArtifacts,
  looksTextual,
  readArtifact,
  resolveInside,
  MAX_INLINE_BYTES,
} from "../src/kernel/artifacts.js";

async function workspace(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "hidane-ws-"));
  await writeFile(join(dir, "summary.md"), "# 摘要\n内容");
  await mkdir(join(dir, "notes"), { recursive: true });
  await writeFile(join(dir, "notes", "detail.md"), "detail");
  await mkdir(join(dir, "node_modules", "pkg"), { recursive: true });
  await writeFile(join(dir, "node_modules", "pkg", "index.js"), "noise");
  await mkdir(join(dir, ".git"), { recursive: true });
  await writeFile(join(dir, ".git", "HEAD"), "ref");
  await writeFile(join(dir, "paper.pdf"), Buffer.from([0x25, 0x50, 0x44, 0x46]));
  return dir;
}

describe("workspace artifacts", () => {
  it("refuses every path that would leave the workspace", async () => {
    const root = "/data/workspaces/wi_x";
    // The path arrives from a URL, so traversal is the threat that matters.
    expect(resolveInside(root, "../../etc/passwd")).toBeNull();
    expect(resolveInside(root, "..")).toBeNull();
    expect(resolveInside(root, "notes/../../../root/.ssh/id_rsa")).toBeNull();
    expect(resolveInside(root, "/etc/passwd")).toBeNull();
    // A sibling directory sharing a name prefix must not pass a prefix check.
    expect(resolveInside(root, "../wi_xy/secret")).toBeNull();
    // Legitimate paths still resolve.
    expect(resolveInside(root, "summary.md")).toBe(`${root}/summary.md`);
    expect(resolveInside(root, "notes/detail.md")).toBe(`${root}/notes/detail.md`);
    expect(resolveInside(root, "./notes/../summary.md")).toBe(`${root}/summary.md`);
  });

  it("lists real work products and skips build noise", async () => {
    const dir = await workspace();
    const files = (await listArtifacts(dir)).map((f) => f.path);
    expect(files).toContain("summary.md");
    expect(files).toContain("notes/detail.md");
    expect(files).toContain("paper.pdf");
    // Dependency and VCS directories are never work products.
    expect(files.some((f) => f.includes("node_modules"))).toBe(false);
    expect(files.some((f) => f.includes(".git"))).toBe(false);
  });

  it("reads text inline and refuses to inline what it should not", async () => {
    const dir = await workspace();
    const md = await readArtifact(dir, "summary.md");
    expect(md?.text).toContain("摘要");

    // Binary is offered as a download rather than mangled into JSON.
    expect((await readArtifact(dir, "paper.pdf"))?.reason).toBe("binary");

    await writeFile(join(dir, "huge.txt"), "x".repeat(MAX_INLINE_BYTES + 1));
    expect((await readArtifact(dir, "huge.txt"))?.reason).toBe("too-large");

    expect(await readArtifact(dir, "../escape.md")).toBeNull();
    expect(await readArtifact(dir, "missing.md")).toBeNull();
  });

  it("classifies extensions the workspace actually produces", () => {
    expect(looksTextual("summary.md")).toBe(true);
    expect(looksTextual("run.sh")).toBe(true);
    expect(looksTextual("Makefile")).toBe(true);
    expect(looksTextual("paper.pdf")).toBe(false);
    expect(looksTextual("shot.png")).toBe(false);
  });
});
