import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, renameSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { CheckoutView, Repo } from "../src/lib/api.js";
import { confirmDialog, expect, say, test, turn, unique, waitForEvent } from "./fixtures.js";

/** A git repository with one commit on main, made without the developer's own git configuration. */
function gitRepo(name: string): string {
  const dir = join(mkdtempSync(join(tmpdir(), "hidane-e2e-repo-")), name);
  mkdirSync(dir);
  const env = {
    ...process.env,
    GIT_CONFIG_GLOBAL: join(dir, "..", "gitconfig"),
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_AUTHOR_NAME: "e2e",
    GIT_AUTHOR_EMAIL: "e2e@example.com",
    GIT_COMMITTER_NAME: "e2e",
    GIT_COMMITTER_EMAIL: "e2e@example.com",
  };
  writeFileSync(join(dir, "README.md"), `# ${name}\n`);
  writeFileSync(join(dir, "hidane.json"), `{"worktree":{"setup":"echo ready > .setup-done"}}\n`);
  writeFileSync(join(dir, ".gitignore"), ".setup-done\n");
  for (const args of [["init", "-q", "-b", "main"], ["add", "-A"], ["commit", "-qm", "init"]]) {
    execFileSync("git", args, { cwd: dir, env });
  }
  return dir;
}

function branches(repo: string): string {
  return execFileSync("git", ["branch", "--list", "hidane/*"], { cwd: repo, encoding: "utf8" });
}

// Archiving uncommitted work is refused with a 409 until the person confirms it.
test.use({ allowedErrors: /status of 409 \(Conflict\)/ });

test("a task in a repository works in its own worktree, shown on its card and archived from the Worktrees list", async ({ page, api }) => {
  const name = unique("blog");
  const repo = gitRepo(name);
  await page.goto("/");
  const messageId = await say(page, `加一个 RSS 页面 ${name} REPO=${repo}`);
  const answered = turn(page, messageId);
  await expect(answered.getByText(/已完成/)).toBeVisible({ timeout: 30_000 });

  const checkouts = (await api.get<{ checkouts: CheckoutView[] }>("/api/checkouts")).checkouts;
  const checkout = checkouts.find((c) => c.repoName === name);
  expect(checkout, "the task's checkout").toBeDefined();
  const c = checkout as CheckoutView;
  expect(c).toMatchObject({ mode: "worktree", base: "main", setup: "done", health: "ok", dirty: 1 });
  expect(c.branch).toBe(`hidane/${c.workItemId}`);
  expect(existsSync(join(c.path, "result.txt")), "the worker wrote into the worktree").toBe(true);
  expect(existsSync(join(c.path, ".setup-done")), "the repo's setup ran first").toBe(true);
  expect(existsSync(join(repo, "result.txt")), "the person's own checkout is untouched").toBe(false);

  // The card names the repository and the branch the work is on.
  const card = answered.getByRole("article");
  await expect(card.getByText(name, { exact: true })).toBeVisible();
  await expect(card.getByText(c.branch, { exact: true })).toBeVisible();

  // Its result waits for review; the task panel shows what changed in the worktree.
  await card.getByRole("button", { name: "展开" }).click();
  const changes = page.getByRole("region", { name: "改动" });
  const file = changes.getByRole("button", { name: "查看 result.txt 的改动" });
  await expect(file).toContainText("未跟踪");
  await file.click();
  await expect(changes.locator("pre")).toContainText(`+加一个 RSS 页面 ${name}`);

  await page.goto("/items");
  await page.getByRole("button", { name: "工作树", exact: true }).click();
  await expect(page).toHaveURL(/\/items\?view=worktrees$/);
  const row = page.locator(`li[data-checkout="${c.id}"]`);
  await expect(row.getByText(name, { exact: true })).toBeVisible();
  await expect(row.getByText("1 处未提交")).toBeVisible();
  await expect(page.locator("li[data-repo]").filter({ hasText: name })).toBeVisible();

  // Uncommitted work is asked about separately before it is thrown away.
  await row.getByRole("button", { name: "归档" }).click();
  await expect(confirmDialog(page)).toContainText("归档这个工作树？");
  await expect(confirmDialog(page)).toContainText(c.branch);
  await confirmDialog(page).getByRole("button", { name: "归档" }).click();
  await expect(confirmDialog(page)).toContainText("有 1 处未提交的改动");
  await confirmDialog(page).getByRole("button", { name: "仍然归档" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "已归档" })).toBeVisible();
  await expect(row).toHaveCount(0);

  expect(existsSync(c.path), "the worktree directory is removed").toBe(false);
  expect(branches(repo), "the branch is kept").toContain(c.branch);
  const archived = await waitForEvent(api, "kind=checkout.archived&tail=50", (e) => e.payload["checkoutId"] === c.id, "checkout.archived");
  expect(archived.workItemId).toBe(c.workItemId);

  // Picking the archived work up again: the new task's card says which branch it continues.
  await page.goto("/");
  const again = await say(page, `接着做 ${name} FROM=${c.workItemId} REPO=${name}`);
  const continued = turn(page, again).getByRole("article");
  await expect(continued.getByText(`接着 ${c.branch}`)).toBeVisible({ timeout: 30_000 });
  await page.goto("/items?view=worktrees");

  // Archived ones are still there to look at.
  await page.getByRole("button", { name: "含已归档" }).click();
  await expect(row.getByText("已归档")).toBeVisible();
  await row.getByRole("button", { name: "进入会话" }).click();
  await expect(page).toHaveURL(new RegExp(`[?&]focus=${c.workItemId}`));
});

test("a repository that went missing is noticed, asked about in the conversation, and can be removed", async ({ page, api }) => {
  const name = unique("notes");
  const repo = gitRepo(name);
  const { repo: registered } = await api.send<{ repo: Repo }>("POST", "/api/repos", { path: repo }, 201);
  renameSync(repo, `${repo}-moved`);

  await page.goto("/items?view=worktrees");
  const row = page.locator(`li[data-repo="${registered.id}"]`);
  await expect(row.getByText("找不到了")).toBeVisible();
  const notice = await waitForEvent(api, "kind=escalation&tail=50", (e) => e.payload["repoId"] === registered.id, "the person is asked");
  expect(notice).toMatchObject({ threadId: "main", payload: { reason: "repo_missing", rootKind: "repo" } });

  await page.goto("/");
  const asked = turn(page, notice.payload["root"] as string);
  await expect(asked.getByText(`仓库检查 · ${name}`)).toBeVisible();
  await expect(asked.getByText(`仓库「${name}」不在 ${registered.path} 了`)).toBeVisible();

  await page.goto("/items?view=worktrees");
  await row.getByRole("button", { name: "移除" }).click();
  await expect(confirmDialog(page)).toContainText(`从列表里移除「${name}」？`);
  await confirmDialog(page).getByRole("button", { name: "移除" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "已移除" })).toBeVisible();
  await expect(row).toHaveCount(0);
  await waitForEvent(api, "kind=repo.forgotten&tail=50", (e) => e.payload["repoId"] === registered.id, "repo.forgotten");
});
