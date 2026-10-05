// Captures every page of the real app (Go backend + fake agent CLIs) in both
// languages and three flavours, so a change to the UI can be looked at — by a
// person reviewing it, or by a coding agent that cannot see a browser —
// instead of inferred from the diff.
//
//   node e2e/screenshots.mjs [outDir]      (make screenshots)
//   ONLY=run-as,settings-roles node e2e/screenshots.mjs   (make screenshots ONLY=run-as,settings-roles)
//
// ONLY takes comma-separated name prefixes and visits only those pages and
// states, so iterating on one part of the UI does not pay for every page;
// files of other pages already in outDir are left as they were.
//
// Build inputs: `make build-nogui fakeagent`. Writes <page>-<lang>-<flavour>.png:
//   desktop  the desktop app's UI (boot.js says desktop) at a window size
//   browser  `hidane serve` in a browser at the same size (token gate, sign out)
//   phone    the browser at phone width
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { chromium } from "@playwright/test";
import { here, pretendDesktop, token, withServer } from "./harness.mjs";

const out = resolve(process.argv[2] ?? join(here, "..", "..", "bin", "screenshots"));
const pages = [
  ["conversation", "/"],
  ["inbox", "/inbox"],
  ["items", "/items"],
  ["worktrees", "/items?view=worktrees"],
  ["schedules", "/schedules"],
  ["memory", "/memory"],
  ["log", "/log"],
  ...["general", "shortcuts", "about", "roles", "providers", "cli", "rules", "status", "events"].map((section) => [`settings-${section}`, `/settings/${section}`]),
];
const only = (process.env.ONLY ?? "").split(",").map((p) => p.trim()).filter(Boolean);
const want = (...names) => only.length === 0 || names.some((name) => only.some((prefix) => name.startsWith(prefix)));
let taken = 0;
const flavours = {
  desktop: { viewport: { width: 1280, height: 820 }, desktop: true },
  browser: { viewport: { width: 1280, height: 820 }, desktop: false },
  phone: { viewport: { width: 390, height: 844 }, desktop: false },
};

await withServer({ offset: 1 }, async (base, api) => {
  // Something on every page: a finished task, a memory, a rule, a schedule.
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "创建一个文件写上 screenshot" }) });
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "你好" }) });
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "给我一个代码示例" }) });
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "画一个流程图" }) });
  await api("/api/memories", { method: "POST", body: JSON.stringify({ kind: "preference", content: "回答保持简洁" }) });
  await api("/api/policies", { method: "POST", body: JSON.stringify({ pattern: "\\bsudo\\b", reason: "no sudo" }) });
  await api("/api/schedules", {
    method: "POST",
    body: JSON.stringify({ name: "daily summary", action: "prompt", cron: "0 18 * * *", timezone: "Asia/Shanghai", spec: { prompt: "hello" } }),
  });
  for (let i = 0; i < 100; i++) {
    const { events } = await api("/api/events?kind=agent.reply&tail=5");
    if (events.some((e) => String(e.payload.text ?? "").startsWith("已完成"))) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  // A task in a repository, so its card names a branch and the Worktrees list has a row.
  const repo = join(mkdtempSync(join(tmpdir(), "hidane-shot-repo-")), "blog");
  mkdirSync(repo);
  writeFileSync(join(repo, "README.md"), "# blog\n");
  const gitEnv = {
    ...process.env,
    GIT_CONFIG_GLOBAL: join(repo, "..", "gitconfig"),
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_AUTHOR_NAME: "shot",
    GIT_AUTHOR_EMAIL: "shot@example.com",
    GIT_COMMITTER_NAME: "shot",
    GIT_COMMITTER_EMAIL: "shot@example.com",
  };
  for (const args of [["init", "-q", "-b", "main"], ["add", "-A"], ["commit", "-qm", "init"]]) execFileSync("git", args, { cwd: repo, env: gitEnv });
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: `给博客加 RSS REPO=${repo}` }) });
  for (let i = 0; i < 100; i++) {
    const { checkouts } = await api("/api/checkouts");
    if (checkouts.length > 0) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  // A second task left open, so the sidebar's in-progress list has something in it.
  await api("/api/work-items", { method: "POST", body: JSON.stringify({ title: "整理本周会议纪要" }) });
  // A question that offers answers to pick, for the queue of what needs the person.
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "订去里斯本的机票 ASK_OPTIONS" }) });
  for (let i = 0; i < 100; i++) {
    const { cards } = await api("/api/board");
    if (cards.some((card) => card.state === "waiting")) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  const { items } = await api("/api/work-items");
  // One message to two tasks at once.
  const pair = items.filter((item) => item.title.includes("screenshot") || item.title.includes("会议纪要")).map((item) => item.id);
  if (pair.length === 2) await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "都在结尾补一行说明", targets: pair }) });
  const task = items.find((item) => item.title.includes("screenshot")) ?? items[0];
  // A task's own memory layer, as the distiller writes it.
  writeFileSync(
    join(task.workspace, "MEMORY.md"),
    `# hidane memory (work item ${task.id})\n\n## decision\n\n- (2026-10-02) 产物统一放在 result.txt <!-- mem_shot01 -->\n`,
  );

  mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const failures = [];
  for (const lang of ["zh", "en"]) {
    for (const [flavour, { viewport, desktop }] of Object.entries(flavours)) {
      const context = await browser.newContext({ viewport, colorScheme: "dark" });
      if (desktop) await pretendDesktop(context, "screenshots");
      await context.addInitScript(
        ([t, l]) => {
          // The sign-in capture clears the token for the rest of the tab's session.
          if (sessionStorage.getItem("signed-out")) localStorage.removeItem("hidane-token");
          else localStorage.setItem("hidane-token", t);
          localStorage.setItem("hidane-lang", l);
          // Every capture starts with the sidebar shown; the collapsed one sets it itself.
          if (!sessionStorage.getItem("keep-sidebar")) localStorage.removeItem("hidane-sidebar-collapsed");
        },
        [token, lang],
      );
      const page = await context.newPage();
      page.on("pageerror", (e) => failures.push(`${lang} ${flavour}: ${e.message}`));
      const shot = async (name) => {
        if (!want(name)) return;
        taken++;
        await page.waitForTimeout(350);
        await page.screenshot({ path: join(out, `${name}-${lang}-${flavour}.png`) });
      };
      const settle = async (path) => {
        await page.goto(base + path);
        await page.waitForLoadState("networkidle");
        await page.waitForTimeout(400);
      };

      for (const [name, path] of pages) {
        if (!want(name)) continue;
        await settle(path);
        await shot(name);
      }

      if (want("composer-mention")) {
        await settle("/");
        await page.getByPlaceholder(lang === "zh" ? "说点什么…" : "Say something…").click();
        await page.keyboard.type("@");
        await shot("composer-mention");
        await page.keyboard.press("Escape");
        await page.keyboard.press("Backspace");
        await page.keyboard.type("/");
        await shot("composer-slash");
        await page.keyboard.press("Backspace");
      }

      if (want("settings-roles-pi-model", "run-as-pi-model")) {
        const saved = await api("/api/settings");
        await api("/api/providers", { method: "POST", body: JSON.stringify({ id: "pi-shot", label: "Pi provider", piProvider: "deepseek" }) });
        const pick = async (trigger, name) => {
          await trigger.click();
          await page.getByRole("listbox").getByRole("option", { name, exact: true }).click();
        };
        if (want("settings-roles-pi-model")) {
          await settle("/settings/roles");
          const row = page.getByRole("group", { name: lang === "zh" ? "Worker（执行）" : "Worker (execution)", exact: true });
          await pick(row.getByLabel("Agent CLI"), "pi");
          await pick(row.getByLabel(lang === "zh" ? "模型服务" : "Provider"), "Pi provider (pi-shot)");
          await row.getByRole("alert").waitFor();
          await row.evaluate((element) => element.scrollIntoView({ block: "center" }));
          await shot("settings-roles-pi-model");
        }
        if (want("run-as-pi-model")) {
          await settle("/");
          await page.getByRole("group", { name: lang === "zh" ? "运行方式" : "Run on" }).getByRole("button").last().click();
          const picker = page.getByRole("dialog");
          await picker.getByRole("button", { name: "pi", exact: true }).click();
          await pick(picker.getByRole("combobox", { name: lang === "zh" ? "模型服务" : "Provider" }), "Pi provider");
          await picker.getByRole("alert").waitFor();
          await shot("run-as-pi-model");
          await page.keyboard.press("Escape");
          await page.evaluate(() => localStorage.removeItem("hidane.runAs"));
        }
        await api("/api/settings/roles", { method: "PUT", body: JSON.stringify({ roles: saved.roles }) });
        await api("/api/providers/pi-shot", { method: "DELETE" });
      }

      // States worth seeing: a task open beside the conversation, the palette,
      // a new task, a message's menu and the confirm it leads to.
      if (task && want("focus")) {
        await settle(`/?focus=${task.id}`);
        await shot("focus");
      }
      if (want("palette", "new-task", "menu", "confirm")) await settle("/");
      if (want("palette")) {
        await page.keyboard.press("ControlOrMeta+k");
        await page.keyboard.type("screenshot");
        await page.waitForTimeout(600);
        await shot("palette");
        await page.keyboard.press("Escape");
      }
      if (want("new-task")) {
        await page.keyboard.press("ControlOrMeta+n");
        // With a title typed, so the dialog shows its primary action enabled.
        await page.keyboard.type(lang === "zh" ? "整理本周会议纪要" : "Summarise this week's meetings");
        await shot("new-task");
        await page.keyboard.press("Escape");
      }
      const said = page.locator("section[data-root] [role=presentation]").first();
      if (want("menu", "confirm") && (await said.count())) {
        await said.click({ button: "right", position: { x: viewport.width < 600 ? 300 : 700, y: 10 } });
        await shot("menu");
        await page.getByRole("menuitem").filter({ hasText: lang === "zh" ? "隐藏" : "Hide" }).click();
        await shot("confirm");
        await page.keyboard.press("Escape");
      }
      // The composer's pickers: who replies, and what a new task runs on — closed, then each open with a favorite.
      if (want("run-as", "run-as-chat", "run-as-task")) {
        await page.evaluate(() => {
          const codex = { agent: "codex", provider: "", model: "gpt-fake-1", effort: "high" };
          localStorage.setItem("hidane.runAs", JSON.stringify(codex));
          localStorage.setItem("hidane.runAsFavorites", JSON.stringify([codex, { agent: "pi", provider: "", model: "fake-model-a", effort: "low" }]));
        });
        await settle("/");
        await shot("run-as");
        const triggers = page.getByRole("group", { name: lang === "zh" ? "运行方式" : "Run on" }).getByRole("button");
        await triggers.first().click();
        await shot("run-as-chat");
        await page.keyboard.press("Escape");
        await triggers.last().click();
        await shot("run-as-task");
        await page.keyboard.press("Escape");
        await page.evaluate(() => {
          localStorage.removeItem("hidane.runAs");
          localStorage.removeItem("hidane.runAsFavorites");
        });
      }
      if (flavour !== "phone" && want("collapsed")) {
        await page.evaluate(() => {
          sessionStorage.setItem("keep-sidebar", "1");
          localStorage.setItem("hidane-sidebar-collapsed", "1");
        });
        await settle("/");
        await shot("collapsed");
        await page.evaluate(() => {
          sessionStorage.removeItem("keep-sidebar");
          localStorage.removeItem("hidane-sidebar-collapsed");
        });
      }
      // A search with nothing to show: the empty state.
      if (want("items-nomatch")) {
        await settle("/items");
        await page.getByRole("searchbox").or(page.locator("main input")).first().fill("zzz-nothing");
        await shot("items-nomatch");
      }
      // The browser asks for the API token; the desktop app has none to ask for.
      if (!desktop && want("signin")) {
        await page.evaluate(() => sessionStorage.setItem("signed-out", "1"));
        await settle("/");
        await shot("signin");
      }
      await context.close();
    }
  }
  await browser.close();
  if (taken === 0) throw new Error(`ONLY=${only.join(",")} matches no screenshot`);
  console.log(`${taken} screenshots in ${out}`);
  return failures;
});
