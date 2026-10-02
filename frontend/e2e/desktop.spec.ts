import type { Page } from "@playwright/test";
import { expect, say, test, turn, unique, waitForEvent, type Api } from "./fixtures.js";

/**
 * The desktop app's own behaviour, run in the desktop projects: `/boot.js`
 * says desktop, and (unless a test turns it off) a fake host answers the
 * `/api/desktop/*` endpoints and records what the page asked of it.
 */
test.beforeEach(({ desktop }) => {
  test.skip(!desktop, "desktop projects only");
});

/** Put the window in the background as far as the page can tell. */
async function sendToBackground(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.hasFocus = () => false;
    window.dispatchEvent(new Event("blur"));
  });
}

async function bringToFront(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.hasFocus = () => true;
    window.dispatchEvent(new Event("focus"));
  });
}

/** A task that finishes on its own, so an `execution.finished` reaches the page. */
async function finishATask(api: Api, marker: string): Promise<void> {
  await api.setAllRoles("claude");
  const { messageId } = await api.send<{ messageId: string }>("POST", "/api/chat", { text: `创建一个文件写上 ${marker}` }, 202);
  const message = await api.event(messageId);
  await waitForEvent(api, `after=${message.seq}`, (e) => e.kind === "execution.finished" && e.payload["root"] === messageId, "the task's execution.finished", 45_000);
}

test("no token gate, no sign-out, no browser-only settings; the live stream still works", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
  await expect(page.getByPlaceholder("token", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "退出" })).toHaveCount(0);
  await expect(page.getByRole("status", { name: /事件流状态/ })).toHaveCount(0);

  // The Wails runtime is absent here: SSE carried the greeting, so an answer arrives live.
  const messageId = await say(page, `你好，${unique("e2e-desktop-live")}`);
  await expect(turn(page, messageId)).toContainText("你好！我是 hidane 的主代理。");

  await page.keyboard.press("ControlOrMeta+Comma");
  await expect(page.getByRole("heading", { name: "通用", level: 1 })).toBeVisible();
  await expect(page.getByRole("switch", { name: "Dock 图标显示未读数" })).toBeVisible();
  await expect(page.getByText("浏览器通知权限")).toHaveCount(0);
  await expect(page.getByRole("region", { name: "账户" })).toHaveCount(0);
});

test("About shows the data directory and opens it in Finder through the host", async ({ page, hostCalls, api }) => {
  await page.goto("/settings/about");
  await expect(page.getByText("桌面应用")).toBeVisible();
  const dir = (await api.settings()).path.replace(/\/[^/]+$/, "");
  await expect(page.getByText(dir, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "在访达中显示" }).click();
  await expect.poll(() => hostCalls.map((call) => call.path)).toContain("/api/desktop/open-data-dir");
  await page.getByRole("button", { name: "复制路径" }).click();
  await expect.poll(() => hostCalls).toContainEqual({ path: "/api/desktop/clipboard", body: { text: dir } });
});

test("a task finishing in the background: a system notification and a Dock badge, cleared on return", async ({ page, api, hostCalls }) => {
  await page.goto("/");
  await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
  await sendToBackground(page);
  await finishATask(api, unique("e2e-badge"));

  await expect.poll(() => hostCalls.filter((call) => call.path === "/api/desktop/notify").map((call) => call.body["title"])).toContain("任务完成");
  await expect.poll(() => hostCalls.filter((call) => call.path === "/api/desktop/badge").at(-1)?.body["count"]).toBeGreaterThanOrEqual(1);
  // The window title is not a tab: it carries no count.
  expect(await page.title()).not.toMatch(/^\(\d+\)/);

  await bringToFront(page);
  await expect.poll(() => hostCalls.filter((call) => call.path === "/api/desktop/badge").at(-1)?.body).toEqual({ count: 0 });
});

test("with notifications and the badge switched off, the host is left alone", async ({ page, api, hostCalls }) => {
  await page.goto("/settings/general");
  await page.getByRole("switch", { name: "任务完成时通知" }).click();
  await page.getByRole("switch", { name: "Dock 图标显示未读数" }).click();
  await expect(page.getByRole("switch", { name: "任务完成时通知" })).toHaveAttribute("aria-checked", "false");
  await page.keyboard.press("Escape");
  await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
  await sendToBackground(page);
  await finishATask(api, unique("e2e-quiet"));
  // Give the event time to reach the page before asserting nothing happened.
  await page.waitForTimeout(1_500);
  expect(hostCalls.filter((call) => call.path === "/api/desktop/notify" || call.path === "/api/desktop/badge")).toEqual([]);
});

test("the webview's own context menu is suppressed, except in fields and on selected text", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
  const prevented = (selector: string) =>
    page.locator(selector).first().evaluate((el) => {
      const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
      el.dispatchEvent(event);
      return event.defaultPrevented;
    });
  expect(await prevented("aside")).toBe(true);
  expect(await prevented("textarea")).toBe(false);
});

test.describe("when the host endpoints fail", () => {
  // No fake host: every /api/desktop/* call is the serve-mode 404 the browser logs.
  test.use({ fakeHost: false, allowedErrors: /status of 404 \(Not Found\)|Failed to load resource/ });

  test("notifications and badges that cannot be delivered never surface as an error", async ({ page, api }) => {
    const failed: string[] = [];
    page.on("response", (response) => {
      if (response.url().includes("/api/desktop/") && response.status() >= 400) failed.push(new URL(response.url()).pathname);
    });
    await page.goto("/");
    await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
    await sendToBackground(page);
    await finishATask(api, unique("e2e-host-down"));
    await expect.poll(() => failed).toContain("/api/desktop/badge");
    await bringToFront(page);
    await page.waitForTimeout(500);
    await expect(page.getByRole("alert")).toHaveCount(0);
  });
});
