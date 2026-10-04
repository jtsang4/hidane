// Captures the README's screenshots (docs/images/) from the real app (Go
// backend + fake agent CLIs) in English, after staging a small story a reader
// can follow: a blog feature on its own branch, a meeting-notes summary, an
// upgrade still running and a question waiting for the person. The fakes speak
// the lines in readme-demo below (FAKEAGENT_DEMO), not their test markers.
//
//   node e2e/readme-shots.mjs [outDir]      (make readme-shots)
//
// Build inputs: `make build-nogui fakeagent`.
import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const here = dirname(fileURLToPath(import.meta.url));
const out = resolve(process.argv[2] ?? join(here, "..", "..", "docs", "images"));
// screenshots.mjs takes n−1; this one n−2.
const port = String(Number(process.env.HIDANE_E2E_PORT || 2797) - 2);
const base = `http://127.0.0.1:${port}`;
const token = "e2e-token";
// A fixed, short root: its paths show up in the pictures.
const root = "/tmp/hidane-readme";
rmSync(root, { recursive: true, force: true });
const demoFile = join(root, "demo.json");

const gitEnv = {
  ...process.env,
  GIT_CONFIG_GLOBAL: join(root, "gitconfig"),
  GIT_CONFIG_NOSYSTEM: "1",
  GIT_AUTHOR_NAME: "demo",
  GIT_AUTHOR_EMAIL: "demo@example.com",
  GIT_COMMITTER_NAME: "demo",
  GIT_COMMITTER_EMAIL: "demo@example.com",
};

/** A small git repository with the given files, committed on main. */
function repo(name, files) {
  const dir = join(root, "code", name);
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(dirname(join(dir, path)), { recursive: true });
    writeFileSync(join(dir, path), content);
  }
  for (const args of [["init", "-q", "-b", "main"], ["add", "-A"], ["commit", "-qm", "Initial commit"]]) execFileSync("git", args, { cwd: dir, env: gitEnv });
  return dir;
}

const blog = repo("blog", {
  "hugo.toml": 'baseURL = "https://mira.example.com/"\ntitle = "Notes from Mira"\n',
  "content/posts/hello.md": "---\ntitle: Hello again\ndate: 2026-09-14\n---\nA fresh start for the blog.\n",
  "content/posts/sqlite.md": "---\ntitle: SQLite is enough\ndate: 2026-09-28\n---\nWhy one file beats a cluster for small tools.\n",
});
const apiRepo = repo("api", {
  "go.mod": "module example.com/api\n\ngo 1.24\n",
  "main.go": "package main\n\nfunc main() {}\n",
  // Long enough to still be running when the pictures are taken.
  Makefile: "test:\n\tsleep 900\n",
});

const demo = {
  replies: [
    {
      when: "What's still open",
      reply:
        "Two things are still moving:\n\n" +
        "- **Upgrade the api service to Go 1.26** — the worker is running the test suite on `hidane/…` in its own worktree.\n" +
        "- **Flights to Lisbon** — waiting on you: Friday evening or Saturday morning?\n\n" +
        "The RSS feed and this week's meeting summary are finished and waiting for your review.",
    },
  ],
  tasks: [
    {
      when: "RSS feed",
      title: "Add an RSS feed to the blog",
      repos: [blog],
      understanding: "Goal: readers can subscribe at /index.xml. Hugo already renders RSS; enable it, link it from the header, cap it at 20 posts.",
      worker: 'WRITE layouts/partials/head-feed.html: <link rel="alternate" type="application/rss+xml" title="Notes from Mira" href="/index.xml">',
      done:
        "Done. The feed is live at **/index.xml**:\n\n" +
        "- RSS enabled in `hugo.toml`, limited to the 20 latest posts\n" +
        "- `<link rel=\"alternate\">` added to the page head so readers find it\n" +
        "- `hugo --minify` builds clean; the feed validates\n\n" +
        "It's on the task's own branch — the worktree is ready for you to review.",
    },
    {
      when: "meeting notes",
      title: "Summarize this week's meeting notes",
      understanding: "Goal: one page from this week's notes — decisions, owners, open questions.",
      worker: "WRITE weekly-summary.md: # Week 40 — decisions, owners, open questions",
      done:
        "Here's the week on one page (saved as `weekly-summary.md`):\n\n" +
        "**Decisions**\n- Ship the beta on Oct 14; feature freeze on the 10th\n- Move the status page to the new provider\n\n" +
        "**Owners**\n- Release notes — Sam\n- Pricing page copy — Alex\n\n" +
        "**Open questions**\n- Do we keep the free tier's export limit?",
    },
    {
      when: "Go 1.26",
      title: "Upgrade the api service to Go 1.26",
      repos: [apiRepo],
      understanding: "Goal: api builds and passes its tests on Go 1.26; fix what the upgrade breaks, nothing more.",
      worker: "RUN: make test",
      done: "Upgraded.",
    },
    {
      when: "Lisbon",
      title: "Flights to Lisbon for the conference",
      understanding: "Goal: two or three flight options to Lisbon arriving before the Oct 22 keynote.",
      ask: "Before I compare fares: do you want to fly out Friday evening or Saturday morning? And is one layover fine if it saves more than €100?",
    },
  ],
};
writeFileSync(demoFile, JSON.stringify(demo));

const server = spawn(process.execPath, [join(here, "serve.mjs"), port], {
  stdio: ["ignore", "ignore", "inherit"],
  env: { ...process.env, FAKEAGENT_DEMO: demoFile, HIDANE_E2E_HOME: join(root, ".hidane") },
});
const stop = () => server.kill("SIGTERM");
process.on("exit", stop);

async function api(path, init = {}) {
  const res = await fetch(base + path, {
    ...init,
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json", ...(init.headers ?? {}) },
  });
  if (!res.ok) throw new Error(`${path}: ${res.status} ${await res.text()}`);
  return res.json();
}
const post = (path, body) => api(path, { method: "POST", body: JSON.stringify(body) });

async function until(what, check) {
  for (let i = 0; i < 300; i++) {
    const found = await check();
    if (found) return found;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`timed out waiting for ${what}`);
}

/** Says something and waits for the answer that names it: from `by`, or a question. */
async function say(text, by = "agent:manager") {
  const { messageId } = await post("/api/chat", { text });
  await until(`an answer to "${text}"`, async () => {
    const { events } = await api("/api/events?tail=100");
    return events.some((e) => e.payload.root === messageId && ((e.kind === "agent.reply" && e.source === by) || e.kind === "escalation"));
  });
}

try {
  await until("the server", async () => {
    try {
      return (await fetch(`${base}/health`)).ok;
    } catch {
      return false;
    }
  });
  await api("/api/settings/roles", {
    method: "PUT",
    body: JSON.stringify({
      roles: {
        primary: { agent: "claude", provider: "", model: "claude-opus-5-5", effort: "high" },
        manager: { agent: "claude", provider: "", model: "claude-sonnet-5-5", effort: "medium" },
        worker: { agent: "codex", provider: "", model: "gpt-5.5", effort: "high" },
        distiller: { agent: "claude", provider: "", model: "claude-haiku-4-5-20251001", effort: "low" },
      },
    }),
  });

  await say("Add an RSS feed to my blog so readers can subscribe — the repo is ~/code/blog.");
  await say("Summarize this week's meeting notes into one page: decisions, owners, open questions.");
  await post("/api/chat", { text: "Upgrade the api service (~/code/api) to Go 1.26 and fix whatever breaks." });
  await until("the upgrade to start", async () => (await api("/api/checkouts")).checkouts.length >= 2);
  await say("Find me flights to Lisbon for the conference — the keynote is on Oct 22.");
  await say("What's still open?", "agent:primary");

  const { items } = await api("/api/work-items");
  const byTitle = (prefix) => items.find((item) => item.title.startsWith(prefix));
  const rss = byTitle("Add an RSS feed");

  for (const [kind, content] of [
    ["preference", "Lead with the result; keep replies under five lines unless I ask for detail."],
    ["preference", "Code changes go on a branch with a pull request — never straight to main."],
    ["fact", "The blog is a Hugo site; `hugo --minify` builds it into public/."],
    ["decision", "Weekly meeting summaries are saved as weekly-summary.md in the task's workspace."],
  ])
    await post("/api/memories", { kind, content });
  writeFileSync(
    join(rss.workspace, "MEMORY.md"),
    `# hidane memory (work item ${rss.id})\n\n## decision\n\n- (2026-10-05) The feed lists the 20 latest posts with their summaries, not full text <!-- mem_demo01 -->\n`,
  );
  await post("/api/policies", { pattern: "\\bgit\\s+push\\b.*\\bmain\\b", reason: "Push to a branch and open a pull request instead" });
  await post("/api/policies", { pattern: "\\bterraform\\s+apply\\b", reason: "Infrastructure changes need my sign-off" });
  await post("/api/schedules", {
    name: "Morning briefing",
    action: "prompt",
    cron: "0 9 * * 1-5",
    timezone: "Europe/Lisbon",
    spec: { prompt: "Summarize what changed overnight in my repos and anything that is waiting on me." },
  });
  await post("/api/schedules", {
    name: "Weekly meeting digest",
    action: "prompt",
    cron: "0 17 * * 5",
    timezone: "Europe/Lisbon",
    spec: { prompt: "Summarize this week's meeting notes into one page: decisions, owners, open questions." },
  });

  mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: "dark" });
  // The desktop app's UI: boot.js says desktop, the host endpoints answer.
  await context.route("**/boot.js", (route) =>
    route.fulfill({ contentType: "application/javascript", body: `window.hidaneBoot = {"desktop":true,"auth":false,"version":"readme"};` }),
  );
  await context.route("**/wails/runtime.js", (route) => route.fulfill({ contentType: "application/javascript", body: "export {};" }));
  await context.route("**/api/desktop/**", (route) => route.fulfill({ contentType: "application/json", body: `{"ok":true}` }));
  await context.addInitScript((t) => {
    localStorage.setItem("hidane-token", t);
    localStorage.setItem("hidane-lang", "en");
  }, token);
  const page = await context.newPage();
  const failures = [];
  page.on("pageerror", (e) => failures.push(e.message));
  const settle = async (path) => {
    await page.goto(base + path);
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(500);
  };
  const shot = async (name) => {
    await page.waitForTimeout(350);
    await page.screenshot({ path: join(out, `${name}.png`) });
  };

  for (const [name, path] of [
    ["chat-task", `/?focus=${rss.id}`],
    ["chat", "/"],
    ["tasks", "/items"],
    ["worktrees", "/items?view=worktrees"],
    ["memory", "/memory"],
    ["schedules", "/schedules"],
    ["settings-roles", "/settings/roles"],
    ["settings-rules", "/settings/rules"],
  ]) {
    await settle(path);
    await shot(name);
  }
  // What a new task runs on: CLI, model and effort, chosen per task.
  await page.evaluate(() => {
    const sonnet = { agent: "claude", provider: "", model: "claude-sonnet-5-5", effort: "high" };
    localStorage.setItem("hidane.runAs", JSON.stringify(sonnet));
    localStorage.setItem(
      "hidane.runAsFavorites",
      JSON.stringify([sonnet, { agent: "claude", provider: "", model: "claude-opus-5-5", effort: "max" }, { agent: "codex", provider: "", model: "gpt-5.5", effort: "high" }]),
    );
  });
  await settle("/");
  await page.getByRole("group", { name: "Run on" }).getByRole("button").last().click();
  await shot("run-on");
  await page.keyboard.press("Escape");

  await browser.close();
  console.log(`README screenshots in ${out}`);
  if (failures.length > 0) {
    console.error(failures.join("\n"));
    process.exitCode = 1;
  }
} finally {
  stop();
}
