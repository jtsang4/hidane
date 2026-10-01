import { expect, test } from "./fixtures.js";

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
    await page.goto("/settings");
    await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
    for (const [kind, label] of [["claude", "Claude Code"], ["codex", "Codex"], ["pi", "pi"]] as const) {
      const row = page.getByRole("group", { name: label, exact: true });
      await expect(row.getByText("可用", { exact: true })).toBeVisible();
      await expect(row.getByText(`版本 ${kind} fake 1.0.0`)).toBeVisible();
      await expect(row.getByText(new RegExp(`路径：.*/bin/fake/${kind}$`))).toBeVisible();
    }
  });

  test("provider from a preset: masked key, role compatibility, refused delete while in use", async ({ page, api }) => {
    await page.goto("/settings");
    await expect(page.getByText("还没有模型服务，角色会使用各 CLI 自己的登录。")).toBeVisible();

    // --- create from the DeepSeek preset ---------------------------------------
    await page.getByLabel("从预设新建…").selectOption("deepseek");
    await expect(page.getByRole("heading", { name: "新建模型服务" })).toBeVisible();
    await expect(page.getByLabel("名称")).toHaveValue("DeepSeek");
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

    // --- assign it to a role running on pi -------------------------------------
    const distiller = page.getByRole("group", { name: "Distiller（记忆提炼）" });
    await distiller.getByLabel("Agent CLI").selectOption("pi");
    await distiller.getByLabel("模型服务").selectOption("deepseek");
    await distiller.getByLabel("模型", { exact: true }).fill("deepseek-v4-pro");
    await expect(distiller.getByText("有未保存的修改")).toBeVisible();
    await expect(distiller.getByRole("alert")).toHaveCount(0);
    await distiller.getByRole("button", { name: "保存" }).click();
    await expect(page.getByRole("alert").filter({ hasText: "角色已保存" })).toBeVisible();
    await expect(distiller.getByText("有未保存的修改")).toHaveCount(0);
    await expect(page.getByText("使用中：Distiller（记忆提炼）")).toBeVisible();
    expect((await api.settings()).roles.distiller).toEqual({ agent: "pi", provider: "deepseek", model: "deepseek-v4-pro", effort: "low" });

    // --- the same provider cannot drive codex: no OpenAI Responses endpoint ------
    const worker = page.getByRole("group", { name: "Worker（执行）" });
    await worker.getByLabel("Agent CLI").selectOption("codex");
    await worker.getByLabel("模型服务").selectOption("deepseek");
    await expect(worker.getByRole("alert")).toHaveText("Codex 需要该服务提供 OpenAI Responses 地址。");
    await expect(worker.getByRole("button", { name: "保存" })).toBeDisabled();
    // The server refuses the same combination on its own.
    const refused = await api.request.put("/api/settings/roles", {
      headers: { authorization: "Bearer e2e-token" },
      data: { roles: { ...(await api.settings()).roles, worker: { agent: "codex", provider: "deepseek", model: "", effort: "low" } } },
    });
    expect(refused.status()).toBe(400);
    expect((await api.settings()).roles.worker).toMatchObject({ agent: "claude", provider: "" });
    // Back to the CLI's own login: compatible again.
    await worker.getByLabel("模型服务").selectOption("");
    await expect(worker.getByRole("alert")).toHaveCount(0);
    await expect(worker.getByRole("button", { name: "保存" })).toBeEnabled();

    // --- deleting a provider a role uses is refused with the server's reason ---
    await page.getByRole("button", { name: "删除 DeepSeek" }).click();
    const inUse = "provider is used by: distiller";
    await expect(page.getByRole("alert").filter({ hasText: inUse }).first()).toBeVisible();
    expect((await api.settings()).providers.map((p) => p.id)).toEqual(["deepseek"]);
    const conflict = await api.request.delete("/api/providers/deepseek", { headers: { authorization: "Bearer e2e-token" } });
    expect(conflict.status()).toBe(409);
    expect(await conflict.json()).toEqual({ ok: false, error: inUse });

    // Free the role, then the delete goes through.
    await distiller.getByLabel("Agent CLI").selectOption("claude");
    await distiller.getByLabel("模型服务").selectOption("");
    await distiller.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("使用中：Distiller（记忆提炼）")).toHaveCount(0);
    await page.getByRole("button", { name: "删除 DeepSeek" }).click();
    await expect(page.getByRole("alert").filter({ hasText: "模型服务已删除" })).toBeVisible();
    await expect(page.getByText(`密钥 ${KEY_HINT}`)).toHaveCount(0);
    expect((await api.settings()).providers).toEqual([]);
  });

  test("role test button makes a real round trip through the fake CLI", async ({ page, api }) => {
    await api.setAllRoles("claude");
    await page.goto("/settings");
    const primary = page.getByRole("group", { name: "Primary（主会话）" });
    await primary.getByRole("button", { name: "测试" }).click();
    const result = primary.getByRole("status");
    await expect(result).toContainText(/成功 · claude · 默认模型 · \d+ ms/);
    await expect(result.locator("pre")).toHaveText("OK");

    // A role on another CLI goes through that CLI.
    await api.setAllRoles("pi");
    await page.reload();
    const manager = page.getByRole("group", { name: "Manager（任务管理）" });
    await manager.getByRole("button", { name: "测试" }).click();
    await expect(manager.getByRole("status")).toContainText(/成功 · pi · 默认模型 · \d+ ms/);
  });
});
