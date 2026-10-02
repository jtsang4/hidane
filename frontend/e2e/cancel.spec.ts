import { SLOW_DELAY_MS, SLOW_URL } from "./env.js";
import { confirmDialog, expect, say, test, turn, unique, waitForEvent } from "./fixtures.js";

// The second backend's fake agents sleep FAKEAGENT_DELAY_MS on every turn, so
// a worker stays running long enough to be stopped from the UI.
test.use({ baseURL: SLOW_URL });
test.setTimeout(6 * SLOW_DELAY_MS + 30_000);

test("cancel: a running task is stopped from its card", async ({ page, api }) => {
  const text = `创建一个文件写上 ${unique("e2e-cancel")}`;
  await page.goto("/");
  const messageId = await say(page, text);
  const question = turn(page, messageId);

  // Primary turn, then Manager turn, then the worker starts and sleeps.
  const card = question.getByRole("article", { name: text });
  await expect(card).toBeVisible({ timeout: 3 * SLOW_DELAY_MS });
  await expect(card.getByText("运行中", { exact: true })).toBeVisible({ timeout: 3 * SLOW_DELAY_MS });
  const message = await api.event(messageId);
  const attributed = await waitForEvent(api, `after=${message.seq}`, (e) => e.kind === "message.attributed" && e.payload["of"] === messageId, "the message's work item");
  const workItemId = attributed.workItemId as string;
  const running = await waitForEvent(api, `item=${workItemId}`, (e) => e.kind === "execution.running", "the worker is running");
  const executionId = running.executionId as string;

  // Running, it is in the sidebar's in-progress list, whose menu offers the same Stop.
  const row = page.getByRole("complementary", { name: "侧边栏" }).getByRole("button", { name: new RegExp(`^${text}`) });
  await expect(row).toBeVisible();
  await row.click({ button: "right" });
  const menu = page.getByRole("menu", { name: `「${text}」的操作` });
  await expect(menu.getByRole("menuitem", { name: "停止" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);

  // Stop asks first; cancelling the question leaves it running.
  await card.getByRole("button", { name: "停止" }).click();
  await expect(confirmDialog(page)).toContainText("停止这个任务？");
  await confirmDialog(page).getByRole("button", { name: "取消" }).click();
  await expect(confirmDialog(page)).toHaveCount(0);
  expect((await api.events(`item=${workItemId}`)).some((e) => e.kind === "execution.cancelled")).toBe(false);

  const stopped = page.waitForResponse((response) => response.url().endsWith(`/api/work-items/${workItemId}/cancel`));
  await card.getByRole("button", { name: "停止" }).click();
  await confirmDialog(page).getByRole("button", { name: "停止" }).click();
  const cancelResponse = await stopped;
  expect(cancelResponse.status()).toBe(200);
  expect(await cancelResponse.json()).toEqual({ ok: true, cancelled: [workItemId] });
  await expect(page.getByRole("alert").filter({ hasText: "已请求停止" })).toBeVisible();

  // The stop is recorded before its effect, and the execution ends as cancelled.
  const cancelled = await waitForEvent(api, `item=${workItemId}`, (e) => e.kind === "execution.cancelled", "execution.cancelled", 5_000);
  expect(cancelled).toMatchObject({ executionId, source: "connector:web", payload: { reason: "cancelled from the web ui" } });
  const finished = await waitForEvent(api, `item=${workItemId}`, (e) => e.kind === "execution.finished" && e.executionId === executionId, "execution.finished", 3 * SLOW_DELAY_MS);
  expect(finished.payload).toMatchObject({ ok: false, cancelled: true, root: messageId });
  expect(finished.seq).toBeGreaterThan(cancelled.seq);
  expect(finished.mailbox).toBe(`manager:${workItemId}`);

  // The Manager hears about it and says so under the question; the card is no longer running.
  await expect(question).toContainText("执行已取消。", { timeout: 3 * SLOW_DELAY_MS });
  await expect(card.getByText("运行中", { exact: true })).toHaveCount(0);
  await expect(card.getByRole("button", { name: "停止" })).toHaveCount(0);
  const replies = (await api.events(`item=${workItemId}`)).filter((e) => e.kind === "agent.reply");
  expect(replies.map((e) => e.payload["root"])).toEqual(replies.map(() => messageId));

  // In the work item's timeline the execution reads as stopped, not failed.
  await card.getByRole("button", { name: "展开" }).click();
  const panel = page.getByRole("complementary", { name: text });
  await expect(panel.getByRole("button", { name: new RegExp(`${executionId}.*已中止`) })).toBeVisible();
  await expect(panel.getByRole("button", { name: new RegExp(`${executionId}.*失败`) })).toHaveCount(0);
});
