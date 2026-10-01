import type { MemoryEntry, PolicyRule } from "../src/lib/api.js";
import { expect, test, unique } from "./fixtures.js";

test("memory: add one, see it listed, forget it", async ({ page, api }) => {
  const content = `回复里提到 ${unique("e2e-memory")} 时用中文`;
  await page.goto("/memory");
  await expect(page.getByRole("heading", { name: "长期记忆" })).toBeVisible();

  await page.getByRole("button", { name: "添加记忆" }).click();
  await page.getByRole("button", { name: "决定", exact: true }).click();
  await page.getByPlaceholder("写一条希望它长期记住的事").fill(content);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "已添加" })).toBeVisible();
  await expect(page.getByText(content)).toBeVisible();

  const { entries, markdown } = await api.get<{ entries: MemoryEntry[]; markdown: string }>("/api/memories");
  const entry = entries.find((candidate) => candidate.content === content);
  expect(entry, "the new memory in MEMORY.md").toMatchObject({ kind: "decision" });
  expect(markdown).toContain(content);
  const id = (entry as MemoryEntry).id;
  const promoted = (await api.events("kind=memory.promoted&tail=20")).find((e) => e.payload["memoryId"] === id);
  expect(promoted?.payload).toMatchObject({ manual: true, scope: "global", content });

  await page.getByRole("button", { name: `遗忘 ${id}` }).click();
  await expect(page.getByRole("alert").filter({ hasText: "已遗忘" })).toBeVisible();
  await expect(page.getByText(content)).toHaveCount(0);

  expect((await api.get<{ entries: MemoryEntry[] }>("/api/memories")).entries.map((e) => e.id)).not.toContain(id);
  const forgotten = (await api.events("kind=memory.forgotten&tail=20")).find((e) => e.payload["memoryId"] === id);
  expect(forgotten, "memory.forgotten event").toBeDefined();
  expect(forgotten?.source).toBe("connector:web");
});

test("policies: add a rule, see it, delete it", async ({ page, api }) => {
  const pattern = unique("e2e-forbidden");
  const reason = `e2e 规则 ${pattern}`;
  await page.goto("/policies");
  await expect(page.getByRole("heading", { name: "规则" })).toBeVisible();

  await page.getByRole("button", { name: "添加规则" }).click();
  await page.getByLabel("匹配（正则，不区分大小写）").fill(pattern);
  await page.getByLabel("拦截理由").fill(reason);
  await page.getByLabel("适用工具").fill("bash, write");
  await page.getByRole("button", { name: "添加规则" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "已添加" })).toBeVisible();
  await expect(page.getByText(pattern, { exact: true })).toBeVisible();
  await expect(page.getByText(reason, { exact: true })).toBeVisible();
  await expect(page.getByText("bash, write", { exact: true })).toBeVisible();

  const { rules } = await api.get<{ rules: PolicyRule[] }>("/api/policies");
  const rule = rules.find((candidate) => candidate.pattern === pattern);
  expect(rule).toMatchObject({ reason, tools: ["bash", "write"] });
  const id = (rule as PolicyRule).id;
  expect((await api.events("kind=policy.added&tail=20")).some((e) => e.payload["ruleId"] === id)).toBe(true);

  await page.getByRole("button", { name: `删除 ${id}` }).click();
  await expect(page.getByText(pattern, { exact: true })).toHaveCount(0);
  expect((await api.get<{ rules: PolicyRule[] }>("/api/policies")).rules.map((r) => r.id)).not.toContain(id);
  expect((await api.events("kind=policy.removed&tail=20")).some((e) => e.payload["ruleId"] === id)).toBe(true);
});
