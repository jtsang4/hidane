/**
 * Live end-to-end verification. Requires: postgres up, pi installed & authed.
 * Run with: pnpm e2e
 *
 * Exercises the full loop with a REAL LLM:
 *   chat → primary routes → work item + workspace → manager plans →
 *   worker executes with tools in the workspace → events → worklog projection
 * plus the background lane: webhook → triage → primary wake.
 */
import { readdir, readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { migrate, closeDb } from "../src/kernel/db.js";
import { listEvents } from "../src/kernel/events.js";
import { listWorkItems } from "../src/kernel/workItems.js";
import { handleUserMessage } from "../src/agents/primary.js";
import { buildApp } from "../src/connectors/http.js";
import { triageOnce } from "../src/connectors/triageLoop.js";
import { renderDay, writeDay, today } from "../src/projections/worklog.js";

let failures = 0;
function check(name: string, ok: boolean, detail = ""): void {
  const mark = ok ? "PASS" : "FAIL";
  if (!ok) failures++;
  console.log(`[${mark}] ${name}${detail ? ` — ${detail}` : ""}`);
}

async function main(): Promise<void> {
  await migrate();
  console.log("== hidane live e2e ==\n");

  // --- 1. fast lane: chat that should create a work item and run a worker ---
  const msg =
    "在工作区里创建一个 python 脚本 hello.py，内容是打印 'hello hidane'，然后运行它并确认输出正确";
  console.log(`chat> ${msg}\n(waiting for primary → manager → worker ...)\n`);
  const outcome = await handleUserMessage(msg);
  console.log(`action: ${outcome.action}, work item: ${outcome.workItemId ?? "-"}`);
  console.log(`reply (first 300 chars): ${outcome.reply.slice(0, 300)}\n`);

  check("primary routed to a new work item", outcome.action === "new_work_item");
  const items = await listWorkItems();
  check("work item persisted", items.length >= 1);

  const item = items.find((i) => i.id === outcome.workItemId) ?? items[0];
  if (item) {
    const ws = await stat(item.workspace);
    check("workspace directory exists", ws.isDirectory(), item.workspace);
    const files = await readdir(item.workspace);
    const hasScript = files.some((f) => f.endsWith(".py"));
    check("worker produced artifact in workspace", hasScript, files.join(", "));
    if (hasScript) {
      const py = files.find((f) => f.endsWith(".py")) as string;
      const content = await readFile(join(item.workspace, py), "utf8");
      check("artifact content mentions hello", /hello/i.test(content));
    }
    const sessions = await readdir(join(item.workspace, ".hidane", "sessions")).catch(() => []);
    check("worker session trace saved in workspace", sessions.length > 0);

    const started = await listEvents({ workItemId: item.id, kind: "execution.started" });
    const finished = await listEvents({ workItemId: item.id, kind: "execution.finished" });
    check("execution.started recorded", started.length >= 1);
    check("execution.finished recorded", finished.length >= 1);
    check(
      "execution finished ok",
      finished.some((e) => e.payload["ok"] === true),
    );
  }

  const routeDecisions = await listEvents({ kind: "route.decision" });
  check("route.decision recorded on main thread", routeDecisions.length >= 1);

  // --- 2. background lane: webhook → triage → primary wake ---
  const app = buildApp();
  const res = await app.request("/webhook/e2e", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ note: "hidane e2e webhook: no action needed, just acknowledge" }),
  });
  check("webhook accepted", res.status === 200);
  const triage = await triageOnce();
  check("triage woke primary for webhook", triage.woke === 1);
  const triageDecisions = await listEvents({ kind: "triage.decision" });
  check(
    "triage decision recorded",
    triageDecisions.some((e) => e.payload["action"] === "wake_primary"),
  );

  // --- 3. health endpoint ---
  const health = await app.request("/health");
  check("health endpoint ok", health.status === 200);

  // --- 4. projection ---
  const md = await renderDay(today());
  check("worklog contains main thread", md.includes("## Main thread"));
  check(
    "worklog contains the work item section",
    item ? md.includes(item.id) : false,
  );
  const path = await writeDay(today());
  check("worklog file written", path.endsWith(".md"), path);

  console.log(`\n== ${failures === 0 ? "ALL PASS" : `${failures} FAILURES`} ==`);
  await closeDb();
  process.exit(failures === 0 ? 0 : 1);
}

main().catch(async (err) => {
  console.error(err);
  await closeDb();
  process.exit(1);
});
