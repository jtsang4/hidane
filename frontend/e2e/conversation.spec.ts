import type { HidaneEvent, WorkItem } from "../src/lib/api.js";
import type { Page } from "@playwright/test";
import { choose, confirmDialog, expect, test, turn, say, unique, waitForEvent } from "./fixtures.js";
import { HISTORY_EVENT_LIMIT } from "../src/lib/history.js";

/** How far the conversation is from its newest message, in px. */
function distanceFromBottom(page: Page): Promise<number> {
  return page.getByRole("log", { name: "会话" }).evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
}

/** How each CLI's tool call shows up in side_effect.intent: claude Write, codex a shell command, pi write. */
const TOOL = { claude: "Write", codex: "bash", pi: "write" } as const;

for (const kind of ["claude", "codex", "pi"] as const) {
  test(`full loop on ${kind}: message → work item → worker writes a file → reply under the question`, async ({ page, api, desktop }) => {
    await api.setAllRoles(kind);
    const marker = unique(`e2e-${kind}`);
    const text = `创建一个文件写上 ${marker}`;

    await page.goto("/");
    const messageId = await say(page, text);
    const question = turn(page, messageId);

    // The Manager's answer lands in the same group as the question that started it.
    await expect(question).toContainText(text);
    await expect(question).toContainText("已完成：", { timeout: 45_000 });
    await expect(question.getByText(`新任务「${text}」`)).toBeVisible();
    if (kind === "claude") {
      // Where the message went can be changed from its line: a menu of open tasks and a new one.
      const change = question.getByRole("button", { name: "改变这条消息的归属" });
      await change.click();
      const routes = page.getByRole("menu");
      await expect(routes.getByRole("menuitem", { name: "新任务" })).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(routes).toBeHidden();
      await expect(change).toBeFocused();
      // Jumping to a day lists today, with this message counted.
      await page.getByRole("button", { name: "跳到某一天" }).click();
      const days = page.getByRole("dialog", { name: "跳到某一天" });
      await expect(days.getByRole("button").first()).toContainText(/条/);
      await page.keyboard.press("Escape");
      await expect(days).toBeHidden();
    }

    // --- the event log is the evidence --------------------------------------
    const message = await api.event(messageId);
    expect(message).toMatchObject({ kind: "user.message", source: "connector:web", threadId: "main", payload: { text } });
    const chain = await api.eventsFrom(message);

    const attributed = chain.find((e) => e.kind === "message.attributed" && e.payload["of"] === messageId);
    expect(attributed, "message.attributed for the message").toBeDefined();
    const workItemId = attributed?.workItemId as string;
    expect(workItemId).toMatch(/^wi_/);
    expect(attributed?.payload).toMatchObject({ created: true, workItemId });

    const route = chain.find((e) => e.kind === "route.decision" && (e.payload["of"] as string[] | undefined)?.includes(messageId));
    const started = chain.find((e) => e.kind === "execution.started" && e.workItemId === workItemId);
    const executionId = started?.executionId as string;
    const intent = chain.find((e) => e.kind === "side_effect.intent" && e.executionId === executionId);
    const result = chain.find((e) => e.kind === "side_effect.result" && e.executionId === executionId);
    const finished = chain.find((e) => e.kind === "execution.finished" && e.executionId === executionId);
    const replies = chain.filter((e) => e.kind === "agent.reply" && e.workItemId === workItemId);
    const done = replies.find((e) => String(e.payload["text"]).startsWith("已完成："));

    expect(route?.payload["effects"]).toEqual([expect.objectContaining({ type: "create_work_item", of: messageId })]);
    expect(started?.payload).toMatchObject({ root: messageId, instructions: `WRITE result.txt: ${text}` });
    expect(executionId).toMatch(/^ex_/);
    expect(intent?.payload["tool"]).toBe(TOOL[kind]);
    expect(String(intent?.payload["input"])).toContain(`${workItemId}/result.txt`);
    expect(result?.payload).toMatchObject({ isError: false });
    expect(finished?.payload).toMatchObject({ ok: true, cancelled: false, root: messageId });
    expect(finished?.mailbox).toBe(`manager:${workItemId}`);
    expect(done, "the Manager's 已完成 reply").toBeDefined();

    // Facts in the order they happened.
    const ordered: (HidaneEvent | undefined)[] = [message, route, attributed, started, intent, result, finished, done];
    const seqs = ordered.map((e) => e?.seq ?? -1);
    expect(seqs).toEqual([...seqs].sort((a, b) => a - b));
    expect(new Set(seqs).size).toBe(seqs.length);

    // Every answer to this message points back at it, so the UI can group them.
    expect(replies.length).toBeGreaterThan(0);
    for (const reply of replies) expect(reply.payload["root"], `root of ${reply.id}`).toBe(messageId);

    // --- the workspace holds what the worker wrote -----------------------------
    const item = (await api.workItems()).find((candidate: WorkItem) => candidate.id === workItemId);
    expect(item?.title).toBe(text);
    const files = await api.get<{ workspace: string; files: { path: string; size: number }[] }>(`/api/work-items/${workItemId}/files`);
    expect(files.files.map((file) => file.path)).toContain("result.txt");
    const file = await api.get<{ text?: string }>(`/api/work-items/${workItemId}/file?path=result.txt`);
    expect(file.text).toContain(marker);

    // --- sitting at the bottom, the view followed every late layout change -----
    // (task card updates, Markdown settling): pinned, nothing laid over the reply.
    await expect.poll(() => distanceFromBottom(page), { message: "pinned to the newest message" }).toBeLessThan(2);
    await expect(page.getByRole("button", { name: "回到最新" })).toHaveCount(0);
    await expect(page.getByText("有新回复")).toHaveCount(0);

    // --- and the UI shows the same work item and its artifact ------------------
    const card = question.getByRole("article", { name: text });
    await expect(card).toBeVisible();
    await card.getByRole("button", { name: "展开" }).click();
    await expect(page).toHaveURL(new RegExp(`focus=${workItemId}`));
    const panel = page.getByRole("complementary", { name: text });
    await expect(panel.getByRole("heading", { name: text })).toBeVisible();
    await expect(panel).toContainText(workItemId);
    await expect(panel.getByRole("region", { name: "对话" })).toContainText("已完成：");

    const artifacts = panel.getByRole("button", { name: /工作区产物/ });
    await expect(artifacts).toContainText(`(${files.files.length})`);
    await artifacts.click();
    const artifact = panel.getByRole("button", { name: "result.txt", exact: true });
    await expect(artifact).toBeVisible();
    // A webview has nowhere to save a download: the desktop app shows the file in Finder instead.
    await expect(panel.getByRole("button", { name: desktop ? "在文件夹中显示 result.txt" : "下载 result.txt" })).toBeVisible();
    await artifact.click();
    await expect(panel.locator("pre").filter({ hasText: marker })).toBeVisible();

    // The execution timeline names the same execution and the tool call it made.
    await expect(panel.getByRole("button", { name: new RegExp(`${executionId}.*成功`) })).toBeVisible();
  });
}

test("small talk gets a reply and creates no work item", async ({ page, api }) => {
  await api.setAllRoles("claude");
  const text = `你好，${unique("e2e-small")}`;
  const itemsBefore = (await api.workItems()).length;

  await page.goto("/");
  const messageId = await say(page, text);
  const question = turn(page, messageId);
  await expect(question).toContainText(text);
  await expect(question).toContainText("你好！我是 hidane 的主代理。");
  await expect(question.getByRole("article")).toHaveCount(0);

  const message = await api.event(messageId);
  const reply = await waitForEvent(api, `after=${message.seq}`, (e) => e.kind === "agent.reply" && e.payload["root"] === messageId, "agent.reply to the greeting");
  expect(reply.workItemId).toBeNull();
  expect(reply.source).toBe("agent:primary");
  const chain = await api.eventsFrom(message);
  const route = chain.find((e) => e.kind === "route.decision" && (e.payload["of"] as string[] | undefined)?.includes(messageId));
  expect(route?.payload["effects"]).toEqual([expect.objectContaining({ type: "reply", of: messageId })]);
  expect(chain.filter((e) => (e.kind === "work_item.created" || e.kind === "message.attributed") && e.payload["of"] === messageId)).toEqual([]);
  expect((await api.workItems()).length).toBe(itemsBefore);
});

test("the composer picks what a task runs on: agent, model and effort, like Paseo", async ({ page, api }) => {
  await api.setAllRoles("claude");
  await page.goto("/");
  const bar = page.getByRole("group", { name: "运行方式" });
  const taskTrigger = bar.getByRole("button", { name: /^任务：/ });
  // By default a task follows the role settings.
  await expect(taskTrigger).toHaveAccessibleName("任务：跟随设置 · Claude Code");
  await taskTrigger.click();
  const picker = page.getByRole("dialog", { name: "新任务的运行方式" });
  await expect(picker).toContainText("用于新任务");
  await expect(picker.getByRole("button", { name: "跟随设置", pressed: true })).toBeFocused();
  await picker.getByRole("button", { name: "Codex" }).click();
  await expect(picker.getByRole("button", { name: "Codex", pressed: true })).toBeVisible();
  // The model list comes from the CLI itself (`codex debug models`, the fake's catalog here).
  await picker.getByRole("button", { name: "显示选项" }).click();
  await expect(page.getByRole("listbox").getByRole("option", { name: /gpt-fake-1/ })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("listbox")).toBeHidden();
  const model = picker.getByRole("combobox", { name: "模型" });
  await model.fill("gpt-fake-mini");
  await model.press("Enter");
  // That model takes only low and medium; the effort list follows it.
  const effort = picker.getByRole("combobox", { name: "推理强度" });
  await effort.click();
  await expect(page.getByRole("listbox").getByRole("option")).toHaveText(["默认", "低", "中"]);
  await page.getByRole("listbox").getByRole("option", { name: "中" }).click();
  await expect(effort).toHaveText("中");
  // The whole combination becomes a favorite.
  await picker.getByRole("button", { name: "收藏当前组合" }).click();
  await expect(picker.getByRole("button", { name: "收藏当前组合", pressed: true })).toBeVisible();
  await expect(picker.getByRole("button", { name: "Codex · gpt-fake-mini · 中", exact: true })).toHaveAttribute("aria-current", "true");
  await page.keyboard.press("Escape");
  await expect(picker).toBeHidden();
  await expect(taskTrigger).toBeFocused();
  await expect(taskTrigger).toHaveAccessibleName("任务：Codex · gpt-fake-mini · 中");

  const text = `创建一个文件写上 ${unique("e2e-runas")}`;
  const messageId = await say(page, text);
  const question = turn(page, messageId);
  await expect(question).toContainText("已完成：", { timeout: 45_000 });
  const card = question.getByRole("article", { name: text });
  await expect(card).toContainText("Codex · gpt-fake-mini · 中");

  const item = (await api.workItems()).find((candidate) => candidate.title === text);
  expect(item?.runAs).toEqual({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "medium" });
  const chain = await api.eventsFrom(await api.event(messageId));
  const intent = chain.find((e) => e.kind === "side_effect.intent" && e.workItemId === item?.id);
  expect(intent?.payload["tool"], "the worker ran on codex").toBe("bash");
  // The task's choice left the role settings alone.
  expect((await api.settings()).roles.worker.agent).toBe("claude");

  // Addressing the task, the picker shows and changes what that task runs on, at once;
  // the conversation picker steps aside, since the task's own Manager answers.
  await card.getByRole("button", { name: "展开" }).click();
  await expect(bar.getByRole("button", { name: /^对话：/ })).toHaveCount(0);
  await expect(taskTrigger).toHaveAccessibleName("任务：Codex · gpt-fake-mini · 中");
  await taskTrigger.click();
  const own = page.getByRole("dialog", { name: "这个任务的运行方式" });
  await expect(own).toContainText("用于这个任务，立即生效");
  await own.getByRole("button", { name: "pi" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "改为使用 pi" })).toBeVisible();
  await expect.poll(async () => (await api.workItems()).find((candidate) => candidate.id === item?.id)?.runAs?.agent).toBe("pi");
  await own.getByRole("button", { name: "跟随设置" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "改回跟随设置" })).toBeVisible();
  await expect.poll(async () => (await api.workItems()).find((candidate) => candidate.id === item?.id)?.runAs ?? null).toBeNull();
  // One click on a favorite puts the whole combination back, and closes the picker.
  await own.getByRole("button", { name: "Codex · gpt-fake-mini · 中", exact: true }).click();
  await expect(own).toBeHidden();
  await expect.poll(async () => (await api.workItems()).find((candidate) => candidate.id === item?.id)?.runAs ?? null).toEqual({
    agent: "codex",
    provider: "",
    model: "gpt-fake-mini",
    effort: "medium",
  });
  // Quick changes, each saved before the task has been read back — and each echoed by the live
  // stream — still end on the last whole combination, and a refetch never wipes what is being typed.
  await taskTrigger.click();
  const ownModel = own.getByRole("combobox", { name: "模型" });
  const ownEffort = own.getByRole("combobox", { name: "推理强度" });
  await ownModel.fill("gpt-fake-1");
  await ownModel.press("Enter");
  await choose(ownEffort, "高");
  await ownModel.fill("gpt-fake-mini");
  await ownModel.press("Enter");
  await choose(ownEffort, "低");
  await expect(ownModel).toHaveValue("gpt-fake-mini");
  await page.keyboard.press("Escape");
  await expect.poll(async () => (await api.workItems()).find((candidate) => candidate.id === item?.id)?.runAs ?? null).toEqual({
    agent: "codex",
    provider: "",
    model: "gpt-fake-mini",
    effort: "low",
  });
  await expect(taskTrigger).toHaveAccessibleName("任务：Codex · gpt-fake-mini · 低");

  // The choice for new tasks, and the favorites, are remembered on this machine.
  await page.goto("/");
  await expect(taskTrigger).toHaveAccessibleName("任务：Codex · gpt-fake-mini · 中");
  await taskTrigger.click();
  await expect(page.getByRole("dialog", { name: "新任务的运行方式" }).getByRole("button", { name: "Codex · gpt-fake-mini · 中", exact: true })).toBeVisible();
});

for (const kind of ["conversation", "new", "task"] as const) {
  test(`the ${kind} picker saves a pi provider only after a model is chosen`, async ({ page, api }) => {
    await api.resetSettings();
    try {
      await api.send("POST", "/api/providers", { id: "pi-test", label: "Pi test", piProvider: "deepseek", models: ["custom-model"] }, 201);
      const item = kind === "task" ? (await api.send<{ item: WorkItem }>("POST", "/api/work-items", { title: unique("pi-picker") }, 201)).item : null;
      const readChoice = async () => {
        if (kind === "conversation") return (await api.settings()).roles.primary;
        if (item) return (await api.workItems()).find((candidate) => candidate.id === item.id)?.runAs ?? null;
        return page.evaluate(() => JSON.parse(localStorage.getItem("hidane.runAs") ?? "null") as unknown);
      };
      await page.goto(item ? `/?focus=${item.id}` : "/");
      const trigger = page.getByRole("group", { name: "运行方式" }).getByRole("button", { name: kind === "conversation" ? /^对话：/ : /^任务：/ });
      const picker = page.getByRole("dialog", { name: kind === "conversation" ? "对话由谁回复" : kind === "new" ? "新任务的运行方式" : "这个任务的运行方式" });
      await trigger.click();
      await picker.getByRole("button", { name: "pi", exact: true }).click();
      await expect.poll(readChoice).toMatchObject({ agent: "pi", provider: "", model: "" });
      await choose(picker.getByRole("combobox", { name: "模型服务" }), "Pi test");
      await expect(picker.getByRole("alert")).toHaveText("pi 选择模型服务后必须填写模型，填写后才会保存。");
      await expect(picker.getByRole("button", { name: "收藏当前组合" })).toBeDisabled();
      expect(await readChoice()).toMatchObject({ provider: "", model: "" });

      // Closing an incomplete choice returns to what is actually saved.
      await page.keyboard.press("Escape");
      await expect(picker).toBeHidden();
      await trigger.click();
      await expect(picker.getByRole("combobox", { name: "模型服务" })).toHaveText("CLI 自己的登录与默认设置");
      await choose(picker.getByRole("combobox", { name: "模型服务" }), "Pi test");
      const model = picker.getByRole("combobox", { name: "模型", exact: true });
      await model.fill("custom-model");
      await model.press("Enter");
      await expect(picker.getByRole("alert")).toHaveCount(0);
      await expect.poll(readChoice).toMatchObject({ agent: "pi", provider: "pi-test", model: "custom-model" });

      // Clearing the required model cannot replace the saved, usable pair.
      await picker.getByRole("button", { name: "显示选项" }).click();
      await page.getByRole("listbox").getByRole("option", { name: "默认模型", exact: true }).click();
      await expect(picker.getByRole("alert")).toBeVisible();
      expect(await readChoice()).toMatchObject({ provider: "pi-test", model: "custom-model" });
      await choose(picker.getByRole("combobox", { name: "模型服务" }), "CLI 自己的登录与默认设置");
      await expect.poll(readChoice).toMatchObject({ provider: "", model: "" });
      await page.keyboard.press("Escape");
    } finally {
      await api.resetSettings();
    }
  });
}

test("the composer switches who replies in the conversation, a favorite switching the whole combination", async ({ page, api }) => {
  await api.setAllRoles("claude");
  await page.goto("/");
  const bar = page.getByRole("group", { name: "运行方式" });
  const chat = bar.getByRole("button", { name: /^对话：/ });
  // It shows the Primary role's setting, not a per-message choice.
  await expect(chat).toHaveAccessibleName("对话：Claude Code · 默认模型 · 低");
  await chat.click();
  const picker = page.getByRole("dialog", { name: "对话由谁回复" });
  await expect(picker).toContainText("所有渠道的对话都由它回复");
  await expect(picker).toContainText("还没有收藏");
  // Nothing to follow: it is the setting.
  await expect(picker.getByRole("button", { name: "跟随设置" })).toHaveCount(0);

  await picker.getByRole("button", { name: "Codex" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "之后的对话由 Codex · 默认模型 回复" })).toBeVisible();
  await expect.poll(async () => (await api.settings()).roles.primary).toEqual({ agent: "codex", provider: "", model: "", effort: "" });
  const model = picker.getByRole("combobox", { name: "模型" });
  await model.fill("gpt-fake-1");
  await model.press("Enter");
  await choose(picker.getByRole("combobox", { name: "推理强度" }), "高");
  await expect.poll(async () => (await api.settings()).roles.primary).toEqual({ agent: "codex", provider: "", model: "gpt-fake-1", effort: "high" });
  await picker.getByRole("button", { name: "收藏当前组合" }).click();

  // Switching again is saved at once; the other roles stay as they were.
  await picker.getByRole("button", { name: "pi" }).click();
  await expect.poll(async () => (await api.settings()).roles.primary.agent).toBe("pi");
  const roles = (await api.settings()).roles;
  expect([roles.manager.agent, roles.worker.agent, roles.distiller.agent]).toEqual(["claude", "claude", "claude"]);
  await expect(chat).toHaveAccessibleName("对话：pi · 默认模型");

  // The conversation goes on, now answered by pi.
  await page.keyboard.press("Escape");
  const text = `你好，${unique("e2e-primary")}`;
  const messageId = await say(page, text);
  await expect(turn(page, messageId)).toContainText("你好！我是 hidane 的主代理。");

  // A favorite puts back agent, model and effort in one click.
  await chat.click();
  await picker.getByRole("button", { name: "Codex · gpt-fake-1 · 高", exact: true }).click();
  await expect(picker).toBeHidden();
  await expect.poll(async () => (await api.settings()).roles.primary).toEqual({ agent: "codex", provider: "", model: "gpt-fake-1", effort: "high" });
  await expect(chat).toHaveAccessibleName("对话：Codex · gpt-fake-1 · 高");

  // Every role is one click further, in Settings.
  await chat.click();
  await picker.getByRole("button", { name: "全部角色设置" }).click();
  await expect(page).toHaveURL(/\/settings\/roles$/);
  await expect(page.locator("#role-primary-agent")).toHaveText("Codex");
  await api.setAllRoles("claude");
});

test("hiding a message through its menu leaves the view following the newest replies", async ({ page, api }) => {
  await api.setAllRoles("claude");
  // History enough to scroll: the bug needs a conversation taller than the view.
  for (let i = 0; i < 40; i++) {
    await api.send("POST", "/api/chat", { text: `你好 ${unique("e2e-history")}` }, 202);
  }
  await page.goto("/");
  let last = "";
  await expect
    .poll(async () => {
      const events = await api.events("kind=agent.reply&tail=1");
      last = events[0]?.id ?? "";
      return (await api.events("kind=agent.reply&tail=60")).length;
    }, { timeout: 60_000 })
    .toBeGreaterThanOrEqual(40);
  await expect(page.locator(`[id^="turn-"]`).last()).toBeVisible();
  await expect.poll(() => distanceFromBottom(page)).toBeLessThan(2);

  const hidden = await say(page, `你好 ${unique("e2e-hide")}`);
  await expect(turn(page, hidden)).toContainText("你好！我是 hidane 的主代理。");
  await turn(page, hidden).getByText(/e2e-hide/).click({ button: "right" });
  await page.getByRole("menu", { name: "消息操作" }).getByRole("menuitem", { name: "隐藏" }).click();
  await confirmDialog(page).getByRole("button", { name: "隐藏" }).click();
  await expect(turn(page, hidden).getByText("这条消息已隐藏")).toBeVisible();

  const task = await say(page, `创建一个文件写上 ${unique("e2e-after-hide")}`);
  await expect(turn(page, task)).toContainText("已完成：", { timeout: 45_000 });
  await expect.poll(() => distanceFromBottom(page), { message: "still pinned to the newest message" }).toBeLessThan(2);
  await expect(page.getByRole("button", { name: "回到最新" })).toHaveCount(0);
  expect(last).not.toBe("");
});

test("a late answer's notice never covers the newest message while the view sits at the bottom", async ({ page, api }) => {
  await api.setAllRoles("claude");
  // Narrow enough that the centred notice overlaps the bubbles' column.
  await page.setViewportSize({ width: 900, height: 700 });
  await page.goto("/");
  const slow = await say(page, `${unique("e2e-cover")} RUN: sleep 4`);
  await expect(turn(page, slow).getByRole("article")).toBeVisible();
  // Push the slow task's turn up out of view, so its answer arrives off-screen.
  let newest = slow;
  for (let i = 0; i < 4; i++) {
    newest = await say(page, `你好 ${unique("e2e-filler")}`);
    await expect(turn(page, newest)).toContainText("你好！我是 hidane 的主代理。");
  }
  const notice = page.getByRole("status").filter({ hasText: "有新回复" });
  await expect(notice).toBeVisible({ timeout: 30_000 });
  await expect.poll(() => distanceFromBottom(page), { message: "still pinned to the newest message" }).toBeLessThan(2);
  await expect
    .poll(async () => {
      const bubble = await turn(page, newest).boundingBox();
      const bar = await notice.boundingBox();
      return bubble && bar ? bar.y - (bubble.y + bubble.height) : -1;
    }, { message: "space between the newest message and the notice" })
    .toBeGreaterThanOrEqual(0);
});

test("a permalink opens the conversation at its message, and back at the live edge the URL lets go of it", async ({ page, api }) => {
  await api.setAllRoles("claude");
  const text = `你好，${unique("e2e-at")}`;
  const { messageId } = await api.send<{ messageId: string }>("POST", "/api/chat", { text }, 202);
  const message = await api.event(messageId);
  await waitForEvent(api, `after=${message.seq}`, (e) => e.kind === "agent.reply" && e.payload["root"] === messageId, "the greeting's reply");

  await page.goto(`/?at=${messageId}`);
  const asked = turn(page, messageId);
  await expect(asked).toContainText(text);
  await expect(asked).toContainText("你好！我是 hidane 的主代理。");
  // The window around it reaches the newest message: that is the live view, which is "at" nothing.
  await expect(page).not.toHaveURL(/at=/);
  await expect(page.getByText("正在查看较早的对话")).toHaveCount(0);
  await expect(asked.getByText("正在判断归属…")).toHaveCount(0);
});

test("long history stays bounded, pages both ways without moving the reader, and reopens evicted messages", async ({ page }) => {
  const history: HidaneEvent[] = Array.from({ length: 1_000 }, (_, i) => ({
    seq: i + 1, id: `history_${i + 1}`, ts: "2026-01-01T00:00:00Z", source: "test",
    kind: "user.message", threadId: "main", workItemId: null, executionId: null,
    payload: { text: `History message ${i + 1}\n${"Long history. ".repeat(20)}` },
  }));
  // A deterministic archive isolates pagination from the shared server's agents.
  await page.route("**/api/events?*", async (route) => {
    const query = new URL(route.request().url()).searchParams;
    if (!query.has("conversation")) return route.continue();
    const limit = Number(query.get("limit"));
    const before = query.get("before");
    const after = query.get("after");
    const around = query.get("around");
    let events = history;
    if (around) {
      const index = history.findIndex((event) => event.id === around);
      events = history.slice(Math.max(0, index - limit / 2), index + limit / 2);
    } else if (before) events = history.filter((event) => event.seq < Number(before)).slice(-limit);
    else if (after) events = history.filter((event) => event.seq > Number(after)).slice(0, limit);
    else events = history.slice(-limit);
    await route.fulfill({ json: { events, hasMore: (events[0]?.seq ?? 1) > 1, hasNewer: (events.at(-1)?.seq ?? 1_000) < 1_000, titles: {} } });
  });
  await page.goto("/");
  const log = page.getByRole("log", { name: "会话" });
  const rows = log.locator("section[data-root]");
  await expect(turn(page, "history_1000")).toBeVisible();

  async function pageThrough(direction: "older" | "newer"): Promise<void> {
    await log.dispatchEvent("wheel");
    await log.evaluate((el, direction) => { el.scrollTop = direction === "older" ? 700 : el.scrollHeight - el.clientHeight - 700; }, direction);
    await expect(page.getByRole("button", { name: "回到最新" })).toBeVisible();
    const anchor = await rows.evaluateAll((nodes) => {
      const top = nodes[0]?.closest('[role="log"]')?.getBoundingClientRect().top ?? 0;
      const node = nodes.find((node) => node.getBoundingClientRect().bottom > top) as HTMLElement | undefined;
      return { root: node?.dataset["root"] ?? "", top: node?.getBoundingClientRect().top ?? 0 };
    });
    const edge = direction === "older" ? rows.first() : rows.last();
    const beforeEdge = await edge.getAttribute("data-root");
    const response = page.waitForResponse((r) => new URL(r.url()).searchParams.has(direction === "older" ? "before" : "after"));
    await page.getByRole("button", { name: direction === "older" ? "加载更早的消息" : "加载更新的消息", exact: true }).dispatchEvent("click");
    await response;
    await expect(edge).not.toHaveAttribute("data-root", beforeEdge!);
    await expect.poll(async () => Math.abs((await turn(page, anchor.root).boundingBox())!.y - anchor.top)).toBeLessThan(2);
    expect(await rows.count()).toBeLessThanOrEqual(HISTORY_EVENT_LIMIT);
  }

  for (let i = 0; i < 6; i++) await pageThrough("older");
  await expect(page.getByText("正在查看较早的对话")).toBeVisible();
  await expect(turn(page, "history_1000")).toHaveCount(0);
  for (let i = 0; i < 2; i++) await pageThrough("newer");
  await page.getByRole("button", { name: "回到最新" }).click();
  await expect(turn(page, "history_1000")).toBeVisible();
  await expect(page.getByText("正在查看较早的对话")).toHaveCount(0);
  await page.goto("/?at=history_10");
  await expect(turn(page, "history_10")).toBeVisible();
  expect(await rows.count()).toBeLessThanOrEqual(HISTORY_EVENT_LIMIT);
});
