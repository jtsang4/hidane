import type { Schedule } from "../src/lib/api.js";
import { expect, test, turn, unique, waitForEvent } from "./fixtures.js";

test("schedules: a prompt schedule runs now and its answer shows up", async ({ page, api }) => {
  await api.setAllRoles("claude");
  const name = unique("e2e-schedule");
  const prompt = `hello from ${name}`;

  await page.goto("/schedules");
  await expect(page.getByRole("heading", { name: "定时任务" })).toBeVisible();
  await page.getByRole("button", { name: "新建" }).click();
  await page.getByPlaceholder("名称，如：每日下午提醒").fill(name);
  await page.getByRole("button", { name: "Agent 任务" }).click();
  await page.getByRole("button", { name: "固定间隔" }).click();
  await page.getByPlaceholder("间隔秒数（≥10）").fill("600");
  await page.getByPlaceholder("要交给 Agent 的任务").fill(prompt);
  await page.getByRole("button", { name: "创建", exact: true }).click();

  const runNow = page.getByRole("button", { name: `立即运行 ${name}` });
  await expect(runNow).toBeVisible();
  await expect(page.getByText(prompt, { exact: true })).toBeVisible();
  await expect(page.getByText("每 600 秒")).toBeVisible();
  const schedule = (await api.get<{ schedules: Schedule[] }>("/api/schedules")).schedules.find((s) => s.name === name);
  expect(schedule).toMatchObject({ action: "prompt", intervalSec: 600, enabled: true, spec: { prompt } });
  const scheduleId = (schedule as Schedule).id;

  const ran = page.waitForResponse((response) => response.url().endsWith(`/api/schedules/${scheduleId}/run`));
  await runNow.click();
  const { status } = (await (await ran).json()) as { status: string };
  expect(status).toMatch(/^posted ev_/);
  await expect(page.getByRole("alert").filter({ hasText: `已运行：${status}` })).toBeVisible();

  // The run history lists the firing.
  await page.getByRole("button", { name: "运行历史" }).click();
  await expect(page.getByText("触发", { exact: true }).first()).toBeVisible();

  // The prompt went to the Primary as a scheduled message, and its answer is in the log…
  const promptId = status.replace(/^posted /, "");
  const fired = await api.event(promptId);
  expect(fired).toMatchObject({ kind: "schedule.prompt", mailbox: "primary", payload: { prompt, scheduleId } });
  const reply = await waitForEvent(api, `after=${fired.seq}`, (e) => e.kind === "agent.reply" && e.payload["root"] === promptId, "the scheduled prompt's reply");
  expect(reply.payload).toMatchObject({ rootKind: "scheduled", rootText: name, text: "你好！我是 hidane 的主代理。" });

  // …and in the conversation, under the schedule's name.
  await page.getByRole("link", { name: "会话" }).click();
  const scheduled = turn(page, promptId);
  await expect(scheduled).toContainText(`定时：${name}`);
  await expect(scheduled).toContainText("你好！我是 hidane 的主代理。");

  // Clean up: the schedule must not fire into later tests.
  await page.getByRole("link", { name: "定时" }).click();
  await page.getByRole("button", { name: `删除 ${name}` }).click();
  await expect(page.getByRole("button", { name: `立即运行 ${name}` })).toHaveCount(0);
  expect((await api.get<{ schedules: Schedule[] }>("/api/schedules")).schedules.map((s) => s.id)).not.toContain(scheduleId);
});

test("status page shows the runtime, the roles and the detected CLIs", async ({ page, api }) => {
  await api.setAllRoles("claude");
  await page.goto("/status");
  await expect(page.getByRole("heading", { name: "运行状态" })).toBeVisible();

  const runtime = page.getByText("运行时", { exact: true }).locator("..");
  await expect(runtime).toContainText("正常");

  const agents = page.getByText("Agent CLI", { exact: true }).locator("..");
  for (const [kind, label] of [["claude", "Claude Code"], ["codex", "Codex"], ["pi", "pi"]] as const) {
    const row = agents.getByRole("listitem").filter({ hasText: `${kind} fake 1.0.0` });
    await expect(row).toContainText(label);
    await expect(row).toContainText("可用");
  }

  const roles = page.getByText("角色分配", { exact: true }).locator("..");
  for (const role of ["Primary（主会话）", "Manager（任务管理）", "Worker（执行）", "Distiller（记忆提炼）"]) {
    await expect(roles.getByRole("listitem").filter({ hasText: role })).toContainText("Claude Code · CLI 自己的登录与默认设置 · 默认模型");
  }

  const status = await api.get<{ runtime: { up: boolean }; agents: { available: boolean }[] }>("/api/status");
  expect(status.runtime.up).toBe(true);
  expect(status.agents.every((agent) => agent.available)).toBe(true);
});
