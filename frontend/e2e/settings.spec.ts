import { choose, confirmDialog, expect, test } from "./fixtures.js";

const KEY = "sk-e2e-provider-secret-7Q2Z";
const KEY_HINT = "…7Q2Z";

test.describe("settings", () => {
  test.beforeEach(async ({ api }) => {
    await api.resetSettings();
  });
  test.afterEach(async ({ api }) => {
    await api.resetSettings();
  });

  test("detects the three fake CLIs", async ({ page }) => {
    await page.goto("/settings/cli");
    await expect(page.getByRole("heading", { name: "Agent CLI", level: 1 })).toBeVisible();
    for (const [kind, label] of [["claude", "Claude Code"], ["codex", "Codex"], ["pi", "pi"]] as const) {
      const row = page.getByRole("group", { name: label, exact: true });
      await expect(row.getByText("可用", { exact: true })).toBeVisible();
      await expect(row.getByText(`版本 ${kind} fake 1.0.0`)).toBeVisible();
      await expect(row.getByText(new RegExp(`路径：.*/bin/fake/${kind}$`))).toBeVisible();
    }
  });

  test("a CLI path saves when the field is left, and only then", async ({ page, api }) => {
    await page.goto("/settings/cli");
    const pi = page.getByRole("group", { name: "pi", exact: true });
    const path = pi.getByRole("textbox", { name: "pi 的绝对路径（留空则自动查找）" });
    const original = (await api.settings()).binaries.pi;
    try {
      await path.fill("/nonexistent/e2e/pi");
      expect((await api.settings()).binaries.pi).toBe(original);
      await path.press("Enter");
      await expect(pi.getByRole("status").filter({ hasText: "已保存" })).toBeVisible();
      expect((await api.settings()).binaries.pi).toBe("/nonexistent/e2e/pi");
      await path.fill(original);
      await path.blur();
      await expect.poll(async () => (await api.settings()).binaries.pi).toBe(original);
    } finally {
      // Later tests drive the fake pi through this path.
      await api.send("PUT", "/api/settings/binaries", { binaries: { pi: original } });
    }
  });

  test.describe("providers", () => {
    // Deleting a provider in use is meant to be refused: the browser logs the 409.
    test.use({ allowedErrors: /status of 409 \(Conflict\)/ });

    test("provider from a preset: masked key, role compatibility, refused delete while in use", async ({ page, api }) => {
      await page.goto("/settings/providers");
      await expect(page.getByRole("heading", { name: "模型服务", level: 1 })).toBeVisible();
      await expect(page.getByText("还没有模型服务，角色会使用各 CLI 自己的登录。")).toBeVisible();

      // --- create from the DeepSeek preset ---------------------------------------
      await choose(page.getByRole("combobox", { name: "从预设新建…" }), "DeepSeek");
      await expect(page.getByRole("heading", { name: "新建模型服务" })).toBeVisible();
      await expect(page.getByRole("textbox", { name: "名称", exact: true })).toHaveValue("DeepSeek");
      await expect(page.getByLabel("ID（可选，留空自动生成）")).toHaveValue("deepseek");
      await expect(page.getByLabel("Anthropic 兼容地址（Claude Code 使用）")).toHaveValue("https://api.deepseek.com/anthropic");
      await expect(page.getByLabel("OpenAI Responses 地址（Codex 使用）")).toHaveValue("");
      await expect(page.getByLabel("pi provider 名称（pi 使用）")).toHaveValue("deepseek");
      await page.getByLabel("API Key").fill(KEY);
      await page.getByRole("button", { name: "创建", exact: true }).click();
      await expect(page.getByRole("alert").filter({ hasText: "模型服务已创建" })).toBeVisible();

      // Only the hint is ever shown — never the key, neither in the page nor in the API.
      await expect(page.getByText(`密钥 ${KEY_HINT}`)).toBeVisible();
      await expect(page.getByText("pi · deepseek")).toBeVisible();
      await expect(page.getByText("Anthropic 兼容", { exact: true })).toBeVisible();
      await expect(page.locator("body")).not.toContainText(KEY);
      const stored = await api.settings();
      expect(stored.providers).toEqual([expect.objectContaining({ id: "deepseek", label: "DeepSeek", hasApiKey: true, apiKeyHint: KEY_HINT })]);
      expect(JSON.stringify(stored)).not.toContain(KEY);
      const recorded = (await api.events("kind=settings.updated&tail=5")).find((e) => e.payload["added"] === "deepseek");
      expect(recorded, "settings.updated for the new provider").toBeDefined();
      expect(JSON.stringify(recorded)).not.toContain(KEY);

      // --- assign it to a role running on pi: every choice saves as it is made ---
      const nav = page.getByRole("navigation", { name: "设置分区" });
      await nav.getByRole("link", { name: "角色" }).click();
      await expect(page).toHaveURL(/\/settings\/roles$/);
      const distiller = page.getByRole("group", { name: "Distiller（记忆提炼）" });
      await choose(distiller.getByLabel("Agent CLI"), "pi");
      await choose(distiller.getByLabel("模型服务"), "DeepSeek (deepseek)");
      const model = distiller.getByLabel("模型", { exact: true });
      await model.fill("deepseek-v4-pro");
      // Typing alone does not save the model…
      await expect.poll(async () => (await api.settings()).roles.distiller.model).toBe("");
      // …Enter does.
      await model.press("Enter");
      await expect(distiller.getByRole("status").filter({ hasText: "已保存" })).toBeVisible();
      await expect(distiller.getByRole("alert")).toHaveCount(0);
      await expect.poll(async () => (await api.settings()).roles.distiller).toEqual({ agent: "pi", provider: "deepseek", model: "deepseek-v4-pro", effort: "low" });

      // --- the same provider cannot drive codex: shown, kept unsaved --------------
      const worker = page.getByRole("group", { name: "Worker（执行）" });
      await choose(worker.getByLabel("Agent CLI"), "Codex");
      await expect.poll(async () => (await api.settings()).roles.worker).toMatchObject({ agent: "codex", provider: "" });
      await choose(worker.getByLabel("模型服务"), "DeepSeek (deepseek)");
      await expect(worker.getByRole("alert")).toHaveText("Codex 需要该服务提供 OpenAI Responses 地址。");
      await expect(worker.getByRole("status").filter({ hasText: "未保存" })).toBeVisible();
      await expect(worker.getByRole("button", { name: "测试" })).toBeDisabled();
      expect((await api.settings()).roles.worker).toMatchObject({ agent: "codex", provider: "" });
      // The server refuses the same combination on its own.
      const refused = await api.request.put("/api/settings/roles", {
        headers: { authorization: "Bearer e2e-token" },
        data: { roles: { worker: { agent: "codex", provider: "deepseek", model: "", effort: "low" } } },
      });
      expect(refused.status()).toBe(400);
      expect((await api.settings()).roles.worker).toMatchObject({ agent: "codex", provider: "" });
      // Back to the CLI's own login: compatible again, and the row is saved.
      await choose(worker.getByLabel("模型服务"), "CLI 自己的登录与默认设置");
      await expect(worker.getByRole("alert")).toHaveCount(0);
      await expect(worker.getByRole("button", { name: "测试" })).toBeEnabled();
      // On its own login the model list is the CLI's catalog (the fake's here), opened by a click in the field.
      await worker.getByRole("combobox", { name: "模型", exact: true }).click();
      await page.getByRole("option", { name: /^gpt-fake-mini/ }).click();
      await expect.poll(async () => (await api.settings()).roles.worker).toMatchObject({ agent: "codex", provider: "", model: "gpt-fake-mini" });

      // --- deleting a provider a role uses is refused with the server's reason ---
      await nav.getByRole("link", { name: "模型服务" }).click();
      await expect(page.getByText("使用中：Distiller（记忆提炼）")).toBeVisible();
      await page.getByRole("button", { name: "删除 DeepSeek" }).click();
      await expect(confirmDialog(page)).toContainText("删除模型服务「DeepSeek」？");
      await confirmDialog(page).getByRole("button", { name: "删除" }).click();
      const inUse = "provider is used by: distiller";
      await expect(page.getByRole("alert").filter({ hasText: inUse }).first()).toBeVisible();
      expect((await api.settings()).providers.map((p) => p.id)).toEqual(["deepseek"]);
      const conflict = await api.request.delete("/api/providers/deepseek", { headers: { authorization: "Bearer e2e-token" } });
      expect(conflict.status()).toBe(409);
      expect(await conflict.json()).toEqual({ ok: false, error: inUse });

      // Free the role, then the delete goes through.
      await nav.getByRole("link", { name: "角色" }).click();
      await choose(distiller.getByLabel("Agent CLI"), "Claude Code");
      await choose(distiller.getByLabel("模型服务"), "CLI 自己的登录与默认设置");
      await expect.poll(async () => (await api.settings()).roles.distiller).toMatchObject({ agent: "claude", provider: "" });
      await nav.getByRole("link", { name: "模型服务" }).click();
      await expect(page.getByText("使用中：Distiller（记忆提炼）")).toHaveCount(0);
      await page.getByRole("button", { name: "删除 DeepSeek" }).click();
      await confirmDialog(page).getByRole("button", { name: "删除" }).click();
      await expect(page.getByRole("alert").filter({ hasText: "模型服务已删除" })).toBeVisible();
      await expect(page.getByText(`密钥 ${KEY_HINT}`)).toHaveCount(0);
      expect((await api.settings()).providers).toEqual([]);
    });
  });

  test("role test button makes a real round trip through the fake CLI", async ({ page, api }) => {
    await api.setAllRoles("claude");
    await page.goto("/settings/roles");
    const primary = page.getByRole("group", { name: "Primary（主会话）" });
    await primary.getByRole("button", { name: "测试" }).click();
    const result = primary.getByRole("status").filter({ hasText: "成功" });
    await expect(result).toContainText(/成功 · claude · 默认模型 · \d+ ms/);
    await expect(result.locator("pre")).toHaveText("OK");

    // A role on another CLI goes through that CLI.
    await api.setAllRoles("pi");
    await page.reload();
    const manager = page.getByRole("group", { name: "Manager（任务管理）" });
    await manager.getByRole("button", { name: "测试" }).click();
    await expect(manager.getByRole("status").filter({ hasText: "成功" })).toContainText(/成功 · pi · 默认模型 · \d+ ms/);
  });
});
