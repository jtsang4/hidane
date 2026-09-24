import { afterEach, describe, expect, it } from "vitest";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import guard from "../extensions/pi-guard.js";
import { buildApp } from "../src/connectors/http.js";

type Handler = (event: { toolName: string; input: unknown }) => Promise<{ block: true; reason: string } | undefined>;

function loadGuard(): Handler {
  let handler: Handler | undefined;
  guard({ on: (_name: string, h: Handler) => (handler = h) } as never);
  return handler!;
}

const env = { ...process.env };
afterEach(() => {
  process.env = { ...env };
});

describe("capture phase (pi guard)", () => {
  it("lets every policy file from the global one down refuse a call", async () => {
    const dir = await mkdtemp(join(tmpdir(), "guard-"));
    const outer = join(dir, "global.json");
    const inner = join(dir, "item.json");
    await writeFile(outer, JSON.stringify({ rules: [{ id: "pol_prod", pattern: "psql .*prod", reason: "生产库只读" }] }));
    await writeFile(inner, JSON.stringify({ rules: [{ id: "pol_env", pattern: "\\.env$", reason: "不要改 .env", tools: ["write", "edit"] }] }));
    process.env["HIDANE_POLICY_FILES"] = `${outer}\n${inner}\n${join(dir, "missing.json")}`;
    const check = loadGuard();
    expect((await check({ toolName: "bash", input: { command: "psql -h prod -c 'DROP TABLE x'" } }))?.reason).toBe(
      "blocked by hidane policy pol_prod: 生产库只读",
    );
    expect((await check({ toolName: "write", input: { path: "app/.env" } }))?.reason).toContain("pol_env");
    // A rule scoped to write/edit does not touch bash.
    expect(await check({ toolName: "bash", input: { command: "cat app/.env" } })).toBeUndefined();
    await rm(dir, { recursive: true });
  });

  it("pauses changes, not reads, while the person's new input is unread", async () => {
    const dir = await mkdtemp(join(tmpdir(), "guard-"));
    const flag = join(dir, "pending-input");
    process.env["HIDANE_PENDING_INPUT_FILE"] = flag;
    const check = loadGuard();
    expect(await check({ toolName: "write", input: { path: "a.txt" } })).toBeUndefined();
    await writeFile(flag, "改用 Python");
    expect((await check({ toolName: "write", input: { path: "a.txt" } }))?.reason).toContain("new input");
    expect((await check({ toolName: "bash", input: { command: "python run.py > out.csv" } }))?.block).toBe(true);
    expect(await check({ toolName: "bash", input: { command: "ls -la" } })).toBeUndefined();
    expect(await check({ toolName: "read", input: { path: "a.txt" } })).toBeUndefined();
    await rm(dir, { recursive: true });
  });

  it("keeps the built-in deny list", async () => {
    const check = loadGuard();
    expect((await check({ toolName: "bash", input: { command: "sudo rm -rf /" } }))?.block).toBe(true);
  });
});

describe("policy api", () => {
  it("adds, lists and removes global rules, rejecting patterns that do not compile", async () => {
    const app = buildApp();
    const bad = await app.request("/api/policies", {
      method: "POST",
      body: JSON.stringify({ pattern: "(", reason: "x" }),
    });
    expect(bad.status).toBe(400);
    const created = await app.request("/api/policies", {
      method: "POST",
      body: JSON.stringify({ pattern: "DROP TABLE", reason: "别删表" }),
    });
    expect(created.status).toBe(201);
    const { rule } = (await created.json()) as { rule: { id: string } };
    const listed = (await (await app.request("/api/policies")).json()) as { rules: { id: string }[] };
    expect(listed.rules.map((r) => r.id)).toContain(rule.id);
    expect((await app.request(`/api/policies/${rule.id}`, { method: "DELETE" })).status).toBe(200);
    expect((await app.request(`/api/policies/${rule.id}`, { method: "DELETE" })).status).toBe(404);
  });
});
