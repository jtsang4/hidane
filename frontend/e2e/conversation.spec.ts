import type { HidaneEvent, WorkItem } from "../src/lib/api.js";
import type { Page } from "@playwright/test";
import { expect, test, turn, say, unique, waitForEvent } from "./fixtures.js";

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
