// Captures every page of the real app (Go backend + fake agent CLIs) in both
// languages at desktop and phone widths, so a change to the UI can be looked
// at — by a person reviewing it, or by a coding agent that cannot see a
// browser — instead of inferred from the diff.
//
//   node e2e/screenshots.mjs [outDir]      (make screenshots)
//
// Build inputs: `make build-nogui fakeagent`. Writes <page>-<lang>-<width>.png.
import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const here = dirname(fileURLToPath(import.meta.url));
const out = resolve(process.argv[2] ?? join(here, "..", "..", "bin", "screenshots"));
const port = "2796";
const base = `http://127.0.0.1:${port}`;
const token = "e2e-token";
const pages = ["/", "/items", "/events", "/log", "/memory", "/schedules", "/policies", "/settings", "/status"];
const viewports = { desktop: { width: 1280, height: 860 }, phone: { width: 390, height: 844 } };

const server = spawn(process.execPath, [join(here, "serve.mjs"), port], { stdio: ["ignore", "ignore", "inherit"] });
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

async function ready() {
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`${base}/health`)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("server did not start");
}

try {
  await ready();
  // Something on every page: a finished task, a memory, a rule, a schedule.
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "创建一个文件写上 screenshot" }) });
  await api("/api/chat", { method: "POST", body: JSON.stringify({ text: "你好" }) });
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

  mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const failures = [];
  for (const lang of ["zh", "en"]) {
    for (const [name, viewport] of Object.entries(viewports)) {
      const context = await browser.newContext({ viewport, colorScheme: "dark" });
      await context.addInitScript(
        ([t, l]) => {
          localStorage.setItem("hidane-token", t);
          localStorage.setItem("hidane-lang", l);
        },
        [token, lang],
      );
      const page = await context.newPage();
      page.on("pageerror", (e) => failures.push(`${lang} ${name}: ${e.message}`));
      for (const path of pages) {
        await page.goto(base + path);
        await page.waitForLoadState("networkidle");
        await page.waitForTimeout(400);
        const slug = path === "/" ? "conversation" : path.slice(1);
        await page.screenshot({ path: join(out, `${slug}-${lang}-${name}.png`) });
      }
      await context.close();
    }
  }
  await browser.close();
  console.log(`screenshots in ${out}`);
  if (failures.length > 0) {
    console.error(failures.join("\n"));
    process.exitCode = 1;
  }
} finally {
  stop();
}
