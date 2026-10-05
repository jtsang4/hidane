import type { BoardCard, HidaneEvent, WorkItem } from "../src/lib/api.js";
import { expect, say, test, turn, unique, waitForEvent, type Api } from "./fixtures.js";
import type { Page } from "@playwright/test";

async function newTask(api: Api, title: string): Promise<WorkItem> {
  return (await api.send<{ item: WorkItem }>("POST", "/api/work-items", { title }, 201)).item;
}

/** Name a task with `@` in the composer: type the start of its title, pick it from the list. */
/** The composer's field; its placeholder changes with whom it addresses. */
const field = (page: Page) => page.getByRole("textbox", { name: "消息", exact: true });

async function mention(page: Page, title: string): Promise<void> {
  const composer = field(page);
  await composer.pressSequentially(`@${title}`);
  const list = page.getByRole("listbox", { name: "选择要发给的任务" });
  await expect(list.getByRole("option", { name: title })).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(list).toBeHidden();
  await expect(page.getByRole("button", { name: `不再发给「${title}」` })).toBeVisible();
}

async function card(api: Api, id: string): Promise<BoardCard | undefined> {
  return (await api.get<{ cards: BoardCard[] }>("/api/board")).cards.find((c) => c.item.id === id);
}

test("one message names two tasks with @; each gets it from its own Manager and answers under it", async ({ page, api }) => {
  const a = await newTask(api, unique("甲任务"));
  const b = await newTask(api, unique("乙任务"));
  await page.goto("/");
  await page.getByPlaceholder("说点什么…").click();
  await mention(page, a.title);
  await mention(page, b.title);
  await expect(field(page)).toHaveAttribute("placeholder", "对这 2 个任务说…");
  // Several tasks keep what each runs on: no picker to change them all at once.
  await expect(page.getByText("多个任务 · 各自的运行方式不变")).toBeVisible();

  const text = unique("都补一行注释");
  await field(page).pressSequentially(text);
  const accepted = page.waitForResponse((r) => r.url().endsWith("/api/chat") && r.request().method() === "POST");
  await page.keyboard.press("Enter");
  const response = await accepted;
  expect(response.request().postDataJSON()).toMatchObject({ text, targets: [a.id, b.id] });
  const { messageId } = (await response.json()) as { messageId: string };
  // The names go with the message; the next one is addressed afresh.
  await expect(page.getByRole("button", { name: `不再发给「${a.title}」` })).toHaveCount(0);

  const said = turn(page, messageId);
  const sentTo = said.getByRole("group", { name: "同时发给" });
  await expect(sentTo.getByRole("button")).toHaveText([a.title, b.title]);

  const replies: HidaneEvent[] = [];
  for (const item of [a, b]) {
    replies.push(
      await waitForEvent(api, "kind=agent.reply&tail=200", (e) => e.workItemId === item.id && e.payload["root"] === messageId && e.payload["report"] === true, `${item.title} reports`, 45_000),
    );
  }

  // Each report is one line under the message, opened in place.
  const show = said.getByRole("button", { name: "展开全文" });
  await expect(show).toHaveCount(2);
  await show.first().click();
  await expect(said.getByRole("button", { name: "收起" })).toBeVisible();
  await expect(said).toContainText(String(replies[0]!.payload["text"]).slice(0, 20));
});

test("a / command acts on the tasks named with @ and says nothing to anyone", async ({ page, api }) => {
  const task = await newTask(api, unique("要收尾的任务"));
  await page.goto("/");
  const composer = field(page);
  await composer.click();
  await composer.pressSequentially("/do");
  const commands = page.getByRole("listbox", { name: "命令" });
  await expect(commands.getByRole("option")).toHaveText([/\/done/]);
  await page.keyboard.press("Enter");
  await expect(composer).toHaveValue("/done ");
  // With nobody named, a command asks for its tasks.
  await page.keyboard.press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "先用 @ 选择要操作的任务" })).toBeVisible();
  await mention(page, task.title);

  const before = (await api.events("kind=user.message&tail=1"))[0];
  await page.keyboard.press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "/done：已处理 1 个任务" })).toBeVisible();
  await expect.poll(async () => (await api.workItems()).find((i) => i.id === task.id)?.status).toBe("done");
  await expect(composer).toHaveValue("");
  const after = await api.events(`kind=user.message&after=${before?.seq ?? 0}`);
  expect(after.filter((e) => String(e.payload["text"]).startsWith("/done")), "a command is not a message").toEqual([]);
});

test("a question's options answer it in one click; results wait under Action required until marked done", async ({ page, api }) => {
  await page.goto("/");
  const messageId = await say(page, `订去里斯本的机票 ASK_OPTIONS ${unique("trip")}`);
  const asked = turn(page, messageId);
  const friday = asked.getByRole("button", { name: "回答：周五晚上" });
  await expect(friday).toBeVisible({ timeout: 30_000 });
  const escalation = await waitForEvent(api, "kind=escalation&tail=100", (e) => e.payload["root"] === messageId, "the question");
  expect(escalation.payload["options"]).toEqual(["周五晚上", "周六早上"]);
  const item = escalation.workItemId as string;
  await expect(page.getByRole("link", { name: /^待处理/ })).toContainText(/\d/);

  await friday.click();
  const answer = await waitForEvent(api, "kind=user.message&tail=100", (e) => e.payload["replyTo"] === escalation.id, "the picked answer");
  expect(answer.payload["text"]).toBe("周五晚上");
  // Answered, the work goes on and its result comes back for review.
  await expect.poll(async () => (await card(api, item))?.state, { timeout: 45_000 }).toBe("review");

  await page.getByRole("link", { name: /^待处理/ }).click();
  await expect(page).toHaveURL(/\/inbox$/);
  const review = page.getByRole("region", { name: "待审阅" }).getByRole("article", { name: (await card(api, item))!.item.title });
  await expect(review).toContainText("最近的回报");
  await expect(review).toContainText("已完成：");
  await review.getByRole("button", { name: "标记完成" }).click();
  await expect(review).toHaveCount(0);
  await expect.poll(async () => (await card(api, item))?.state).toBe("done");
});

test("a question is answered in words from the Action required page", async ({ page, api }) => {
  await page.goto("/");
  const messageId = await say(page, `订去东京的机票 ASK_OPTIONS ${unique("trip")}`);
  const escalation = await waitForEvent(api, "kind=escalation&tail=100", (e) => e.payload["root"] === messageId, "the question", 30_000);
  const title = (await card(api, escalation.workItemId as string))!.item.title;

  await page.goto("/inbox");
  const question = page.getByRole("region", { name: "待回答" }).getByRole("article", { name: title });
  await expect(question).toContainText("出发时间选哪个？");
  await expect(question.getByRole("button", { name: "回答：周六早上" })).toBeVisible();
  const typed = question.getByRole("textbox", { name: `回答「${title}」` });
  await typed.fill("周日上午也可以");
  await typed.press("Enter");
  await expect(page.getByRole("alert").filter({ hasText: "已回答" })).toBeVisible();
  const answer = await waitForEvent(api, "kind=user.message&tail=100", (e) => e.payload["replyTo"] === escalation.id, "the typed answer");
  expect(answer.payload["text"]).toBe("周日上午也可以");
  await expect(question).toHaveCount(0, { timeout: 15_000 });
});

test("something the Primary answered becomes a task from its message menu", async ({ page, api }) => {
  await page.goto("/");
  const words = `你好 ${unique("promote")}`;
  const messageId = await say(page, words);
  const said = turn(page, messageId);
  const reply = said.getByText("你好！我是 hidane 的主代理。");
  await expect(reply).toBeVisible({ timeout: 30_000 });
  await reply.click({ button: "right" });
  const menu = page.getByRole("menu", { name: "消息操作" });
  await menu.getByRole("menuitem", { name: "转成任务" }).click();
  await expect(page.getByRole("alert").filter({ hasText: `已转成任务「${words}」` })).toBeVisible();

  const attributed = await waitForEvent(api, "kind=message.attributed&tail=100", (e) => e.payload["of"] === messageId, "the new task");
  expect(attributed.payload).toMatchObject({ by: "user", created: true });
  await expect(page).toHaveURL(new RegExp(`focus=${attributed.workItemId}`));
  await expect(page.getByRole("complementary", { name: words })).toBeVisible();
  // Once a task, there is nothing left to promote.
  await reply.click({ button: "right" });
  await expect(menu.getByRole("menuitem", { name: "转成任务" })).toHaveCount(0);
  await page.keyboard.press("Escape");
});
