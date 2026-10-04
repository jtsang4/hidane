import type { Page } from "@playwright/test";
import type { MemoryEntry, WorkItem } from "../src/lib/api.js";
import { confirmDialog, expect, say, test, turn, unique, waitForEvent } from "./fixtures.js";

/** The window shell: settings as a surface of its own, the palette, ⌘N, menus, the sidebar. */

const sidebar = (page: Page) => page.getByRole("complementary", { name: "侧边栏" });

test("settings opens with ⌘, and leaves with Esc, ⌘, or Back — to the page it was opened from", async ({ page }) => {
  await page.goto("/memory");
  await expect(page.getByRole("heading", { name: "长期记忆", level: 1 })).toBeVisible();

  await page.keyboard.press("ControlOrMeta+Comma");
  await expect(page).toHaveURL(/\/settings\/general$/);
  await expect(page.getByRole("heading", { name: "通用", level: 1 })).toBeVisible();
  // A surface of its own: the main window's sidebar is gone.
  await expect(sidebar(page)).toHaveCount(0);
  // Moving between sections does not change where Esc goes back to.
  await page.getByRole("navigation", { name: "设置分区" }).getByRole("link", { name: "角色" }).click();
  await expect(page).toHaveURL(/\/settings\/roles$/);
  // Esc in an open list closes the list, not settings.
  await page.getByRole("group", { name: "Primary（主会话）" }).getByRole("combobox", { name: "Agent CLI" }).click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("listbox")).toBeHidden();
  await expect(page).toHaveURL(/\/settings\/roles$/);
  await page.getByRole("navigation", { name: "设置分区" }).getByRole("link", { name: "事件日志" }).click();
  // Esc in a field lets go of the field first, then leaves.
  const search = page.getByRole("textbox", { name: "在已加载的事件里搜索…" });
  await search.click();
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/settings\/events$/);
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/\/memory$/);
  await expect(page.getByRole("heading", { name: "长期记忆", level: 1 })).toBeVisible();

  // ⌘, toggles.
  await page.goto("/schedules");
  await page.keyboard.press("ControlOrMeta+Comma");
  await expect(page).toHaveURL(/\/settings\/general$/);
  await page.keyboard.press("ControlOrMeta+Comma");
  await expect(page).toHaveURL(/\/schedules$/);

  // The sidebar's gear and the Back button.
  await page.goto("/items");
  await sidebar(page).getByRole("button", { name: "设置" }).click();
  await expect(page).toHaveURL(/\/settings\/general$/);
  await page.getByRole("button", { name: "返回" }).click();
  await expect(page).toHaveURL(/\/items$/);
});

test("⌘K palette: commands go places, and the search finds tasks and messages", async ({ page, api }) => {
  await api.setAllRoles("claude");
  const title = unique("e2e-palette-task");
  const { item } = await api.send<{ item: WorkItem }>("POST", "/api/work-items", { title }, 201);
  const said = `你好 ${unique("e2e-palette-said")}`;
  const { messageId } = await api.send<{ messageId: string }>("POST", "/api/chat", { text: said }, 202);

  await page.goto("/");
  const palette = page.getByRole("dialog", { name: "命令面板" });
  const input = palette.getByRole("combobox");

  // A command, by keyboard.
  await page.keyboard.press("ControlOrMeta+k");
  await expect(input).toBeFocused();
  await input.fill("定时");
  await expect(palette.getByRole("option").first()).toContainText("前往定时");
  await input.press("Enter");
  await expect(palette).toHaveCount(0);
  await expect(page).toHaveURL(/\/schedules$/);

  // A settings section, by English keyword in the Chinese UI, with the mouse.
  await sidebar(page).getByRole("button", { name: "搜索" }).click();
  await input.fill("providers");
  await palette.getByRole("option", { name: /设置：模型服务/ }).click();
  await expect(page).toHaveURL(/\/settings\/providers$/);
  await page.keyboard.press("Escape");

  // A work item from the search: it opens beside the conversation.
  await page.keyboard.press("ControlOrMeta+k");
  await input.fill(title);
  const task = palette.getByRole("option", { name: new RegExp(title) });
  await expect(task).toBeVisible();
  await task.click();
  await expect(page).toHaveURL(new RegExp(`focus=${item.id}`));
  await expect(page.getByRole("complementary", { name: title })).toBeVisible();

  // A message: the conversation opens at it.
  await page.keyboard.press("ControlOrMeta+k");
  await input.fill(said);
  const message = palette.getByRole("option", { name: new RegExp(said.split(" ")[1]!) });
  await expect(message).toBeVisible();
  // Nothing else matches: the message is first, and selected.
  await expect(message).toHaveAttribute("aria-selected", "true");
  await input.press("Enter");
  await expect(turn(page, messageId)).toContainText(said);

  // Esc closes it without doing anything.
  await page.keyboard.press("ControlOrMeta+k");
  await expect(palette).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(palette).toHaveCount(0);
});

test("⌘N creates a work item and opens it", async ({ page, api }) => {
  const title = unique("e2e-new-task");
  await page.goto("/items");
  await page.keyboard.press("ControlOrMeta+n");
  const dialog = page.getByRole("dialog", { name: "新任务" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("textbox", { name: "标题" })).toBeFocused();
  // Esc throws it away.
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);

  await page.keyboard.press("ControlOrMeta+n");
  await dialog.getByRole("textbox", { name: "标题" }).fill(title);
  await dialog.getByRole("textbox", { name: "标题" }).press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "任务已创建" })).toBeVisible();
  await expect(dialog).toHaveCount(0);
  const created = (await api.workItems()).find((candidate) => candidate.title === title);
  expect(created, "the work item in the API").toMatchObject({ status: "open" });
  await expect(page).toHaveURL(new RegExp(`focus=${created?.id}`));
  expect((await api.events(`item=${created?.id}`)).some((e) => e.kind === "work_item.created")).toBe(true);
});

test("⌘L goes to the conversation and puts the cursor in the composer", async ({ page }) => {
  await page.goto("/log");
  await page.keyboard.press("ControlOrMeta+l");
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByPlaceholder("说点什么…")).toBeFocused();
});

test("the work log picks its day from a calendar", async ({ page }) => {
  await page.goto("/log");
  const today = page.getByRole("button", { name: "今天", exact: true });
  await expect(today).toBeDisabled();
  const day = page.getByRole("button", { name: /^日期 / });
  const shown = (await day.getAttribute("aria-label")) ?? "";
  await day.click();
  const calendar = page.getByRole("dialog", { name: "日期" });
  await expect(calendar.getByRole("button", { name: "上个月" })).toBeVisible();
  // Today has the focus; the day before it is one key away.
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("Enter");
  await expect(calendar).toBeHidden();
  await expect(day).not.toHaveAccessibleName(shown);
  await expect(today).toBeEnabled();
  await today.click();
  await expect(day).toHaveAccessibleName(shown);
});

test("confirm dialog: cancelling does nothing, confirming does it", async ({ page, api }) => {
  const content = `确认框测试 ${unique("e2e-confirm")}`;
  const { entry } = await api.send<{ entry: MemoryEntry }>("POST", "/api/memories", { kind: "fact", content }, 201);
  await page.goto("/memory");
  const forget = page.getByRole("button", { name: `遗忘 ${entry.id}` });

  await forget.click();
  await expect(confirmDialog(page)).toBeVisible();
  await confirmDialog(page).getByRole("button", { name: "取消" }).click();
  await expect(confirmDialog(page)).toHaveCount(0);
  await expect(page.getByText(content)).toBeVisible();

  // From the keyboard, Esc gives focus back to the button that asked.
  await forget.focus();
  await page.keyboard.press("Enter");
  await expect(confirmDialog(page)).toBeVisible();
  // Tab stays inside the dialog, round from the last button to the first and back.
  const confirmButton = confirmDialog(page).getByRole("button", { name: "遗忘" });
  await expect(confirmButton).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(confirmDialog(page).getByRole("button", { name: "取消" })).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(confirmButton).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(confirmDialog(page)).toHaveCount(0);
  await expect(forget).toBeFocused();
  expect((await api.get<{ entries: MemoryEntry[] }>("/api/memories")).entries.map((e) => e.id)).toContain(entry.id);

  // Enter answers the button that has focus: the confirm button, when it opens.
  await forget.click();
  await expect(confirmDialog(page).getByRole("button", { name: "遗忘" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByText(content)).toHaveCount(0);
  expect((await api.get<{ entries: MemoryEntry[] }>("/api/memories")).entries.map((e) => e.id)).not.toContain(entry.id);
});

test("a message's menu, from a right click or its ⋯ button: copy the text, hide it", async ({ page, api, desktop, hostCalls }) => {
  await api.setAllRoles("claude");
  if (!desktop) {
    await page.addInitScript(() => {
      const copied: string[] = [];
      (window as unknown as { __copied: string[] }).__copied = copied;
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (text: string) => void copied.push(text) } });
    });
  }
  const text = `你好，${unique("e2e-menu")}`;
  await page.goto("/");
  const messageId = await say(page, text);
  const asked = turn(page, messageId);
  await expect(asked).toContainText("你好！我是 hidane 的主代理。");

  const bubble = asked.getByText(text, { exact: true });
  await bubble.click({ button: "right" });
  const menu = page.getByRole("menu", { name: "消息操作" });
  // The desktop app has no address bar to paste a permalink into.
  await expect(menu.getByRole("menuitem")).toHaveText(desktop ? ["复制文本", "隐藏"] : ["复制文本", "复制链接", "隐藏"]);
  await expect(menu.getByRole("menuitem").first()).toBeFocused();
  await menu.getByRole("menuitem", { name: "复制文本" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "已复制" })).toBeVisible();
  if (desktop) {
    expect(hostCalls).toContainEqual({ path: "/api/desktop/clipboard", body: { text } });
  } else {
    expect(await page.evaluate(() => (window as unknown as { __copied: string[] }).__copied)).toEqual([text]);
  }

  // The ⋯ button opens the same menu; Esc closes it.
  await bubble.hover();
  await asked.getByRole("button", { name: "消息的更多操作" }).first().click();
  await expect(menu.getByRole("menuitem")).toHaveCount(desktop ? 2 : 3);
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);

  // Hide asks first.
  await bubble.click({ button: "right" });
  await menu.getByRole("menuitem", { name: "隐藏" }).click();
  await expect(confirmDialog(page)).toContainText("隐藏这条消息？");
  await confirmDialog(page).getByRole("button", { name: "隐藏" }).click();
  await expect(asked.getByText("这条消息已隐藏")).toBeVisible();
  await expect(asked.getByText(text, { exact: true })).toHaveCount(0);
  const message = await api.event(messageId);
  await waitForEvent(api, `after=${message.seq}`, (e) => e.kind === "message.redacted" && JSON.stringify(e.payload).includes(messageId), "message.redacted");
});

test("a code block in a reply: its copy button copies the code and says so", async ({ page, api, desktop, hostCalls }) => {
  await api.setAllRoles("claude");
  if (!desktop) {
    await page.addInitScript(() => {
      const copied: string[] = [];
      (window as unknown as { __copied: string[] }).__copied = copied;
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (text: string) => void copied.push(text) } });
    });
  }
  await page.goto("/");
  const asked = turn(page, await say(page, `给我一个代码示例 ${unique("e2e-code")}`));
  const block = asked.locator("pre").filter({ hasText: "make test" });
  await expect(block).toBeVisible();

  const copy = asked.getByRole("button", { name: "复制代码" });
  // Out of the way until the pointer comes to the block (hovered again while the new reply still rises in or scrolls).
  await expect(copy).toHaveCSS("opacity", "0");
  await expect(async () => {
    await block.hover();
    await expect(copy).toHaveCSS("opacity", "1", { timeout: 1_000 });
  }).toPass();
  await copy.click();
  await expect(asked.getByRole("button", { name: "已复制" })).toBeVisible();
  const code = "make test\npnpm -C frontend test";
  if (desktop) {
    expect(hostCalls).toContainEqual({ path: "/api/desktop/clipboard", body: { text: code } });
  } else {
    expect(await page.evaluate(() => (window as unknown as { __copied: string[] }).__copied)).toEqual([code]);
  }
  await expect(asked.getByRole("button", { name: "复制代码" })).toBeVisible();
});

test("a task's menu: open, mark done, archive — and in the desktop app, show its workspace", async ({ page, api, desktop, hostCalls }) => {
  const title = unique("e2e-task-menu");
  const { item } = await api.send<{ item: WorkItem }>("POST", "/api/work-items", { title }, 201);
  const status = async () => (await api.workItems()).find((candidate) => candidate.id === item.id)?.status;
  await page.goto("/items");
  const row = page.getByRole("link", { name: new RegExp(title) });
  await row.click({ button: "right" });
  const menu = page.getByRole("menu", { name: `「${title}」的操作` });
  await expect(menu.getByRole("menuitem")).toHaveText(desktop ? ["展开", "标记完成", "归档", "在访达中显示工作区"] : ["展开", "标记完成", "归档"]);
  if (desktop) {
    await menu.getByRole("menuitem", { name: "在访达中显示工作区" }).click();
    await expect.poll(() => hostCalls).toContainEqual({ path: `/api/work-items/${item.id}/reveal`, body: { path: "" } });
  } else {
    await page.keyboard.press("Escape");
  }
  await expect(menu).toHaveCount(0);

  // The ⋯ button offers the same menu.
  await row.hover();
  await page.getByRole("button", { name: `「${title}」的更多操作` }).click();
  await menu.getByRole("menuitem", { name: "标记完成" }).click();
  await expect.poll(status).toBe("done");

  // Done leaves the default list; with everything shown, it can still be archived — after a question.
  await expect(row).toHaveCount(0);
  await page.getByRole("button", { name: "含已归档" }).click();
  await row.click({ button: "right" });
  await expect(menu.getByRole("menuitem", { name: "标记完成" })).toHaveCount(0);
  await menu.getByRole("menuitem", { name: "归档" }).click();
  await confirmDialog(page).getByRole("button", { name: "取消" }).click();
  expect(await status()).toBe("done");
  await row.click({ button: "right" });
  await menu.getByRole("menuitem", { name: "归档" }).click();
  await confirmDialog(page).getByRole("button", { name: "归档" }).click();
  await expect.poll(status).toBe("closed");

  // Open goes to the focus panel.
  await row.click({ button: "right" });
  await menu.getByRole("menuitem", { name: "展开" }).click();
  await expect(page).toHaveURL(new RegExp(`focus=${item.id}`));
  await expect(page.getByRole("complementary", { name: title })).toBeVisible();
});

test("⌘B collapses the sidebar, and it stays that way after a reload", async ({ page }) => {
  await page.goto("/");
  await expect(sidebar(page)).toBeVisible();
  await page.keyboard.press("ControlOrMeta+b");
  await expect(sidebar(page)).toHaveCount(0);
  const expand = page.getByRole("button", { name: "展开侧边栏" });
  await expect(expand).toBeVisible();
  await page.reload();
  await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
  await expect(sidebar(page)).toHaveCount(0);
  // One press, one toggle.
  await page.keyboard.press("ControlOrMeta+b");
  await expect(sidebar(page)).toBeVisible();
  await page.reload();
  await expect(sidebar(page)).toBeVisible();
  // The toolbar and the sidebar's own buttons do the same.
  await sidebar(page).getByRole("button", { name: "收起侧边栏" }).click();
  await expect(sidebar(page)).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => localStorage.getItem("hidane-sidebar-collapsed"))).toBe("1");
  await expect(expand).toBeVisible();
  await expand.click();
  await expect(sidebar(page)).toBeVisible();
});

test("an image dragged onto the conversation is attached to the next message", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByPlaceholder("说点什么…")).toBeVisible();
  const transfer = await page.evaluateHandle(() => {
    const data = new DataTransfer();
    // A 1×1 transparent PNG.
    const bytes = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="), (c) => c.charCodeAt(0));
    data.items.add(new File([bytes], "dropped.png", { type: "image/png" }));
    return data;
  });
  const region = page.getByRole("region", { name: "会话" });
  await region.dispatchEvent("dragenter", { dataTransfer: transfer });
  await region.dispatchEvent("dragover", { dataTransfer: transfer });
  await expect(page.getByText("松开以添加图片")).toBeVisible();
  await region.dispatchEvent("drop", { dataTransfer: transfer });
  await expect(page.getByText("松开以添加图片")).toHaveCount(0);
  await expect(page.getByRole("img", { name: "dropped.png" })).toBeVisible();
  await page.getByRole("button", { name: "移除图片 dropped.png" }).click();
  await expect(page.getByRole("img", { name: "dropped.png" })).toHaveCount(0);
});
