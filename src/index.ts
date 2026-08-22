#!/usr/bin/env node
import { Command } from "commander";
import { config } from "./config.js";
import { migrate, closeDb } from "./kernel/db.js";
import { listEvents } from "./kernel/events.js";
import { listWorkItems } from "./kernel/workItems.js";
import { handleUserMessage } from "./agents/primary.js";
import { disposeAgents } from "./agents/sdk.js";
import { startHeartbeat } from "./connectors/timer.js";
import { startHttp } from "./connectors/http.js";
import { startTriageLoop } from "./connectors/triageLoop.js";
import { renderDay, writeDay, today } from "./projections/worklog.js";

const program = new Command();
program
  .name("hidane")
  .description("hidane (火種) — persistent personal agent runtime")
  .version("0.1.0");

program
  .command("init")
  .description("create/upgrade the database schema")
  .action(async () => {
    await migrate();
    console.log("schema ready");
    await closeDb();
  });

program
  .command("chat")
  .description("send a message to the Primary agent (fast lane)")
  .argument("<message...>", "message text")
  .action(async (parts: string[]) => {
    await migrate();
    const outcome = await handleUserMessage(parts.join(" "));
    console.log(`\n[${outcome.action}${outcome.workItemId ? ` → ${outcome.workItemId}` : ""}]`);
    console.log(outcome.reply);
    await disposeAgents();
    await closeDb();
    process.exit(0);
  });

program
  .command("items")
  .description("list work items")
  .option("-a, --all", "include non-open items")
  .action(async (opts: { all?: boolean }) => {
    await migrate();
    const items = await listWorkItems(opts.all ? undefined : "open");
    if (items.length === 0) console.log("(no work items)");
    for (const i of items) {
      console.log(`${i.id}  [${i.status}]  ${i.title}`);
      console.log(`  thread: ${i.threadId}  workspace: ${i.workspace}`);
    }
    await closeDb();
  });

program
  .command("events")
  .description("show events from the log")
  .option("-t, --tail <n>", "last n events", "20")
  .option("--thread <id>", "filter by thread")
  .option("--item <id>", "filter by work item")
  .action(async (opts: { tail: string; thread?: string; item?: string }) => {
    await migrate();
    const events = await listEvents({
      tail: Number(opts.tail),
      threadId: opts.thread,
      workItemId: opts.item,
    });
    for (const e of events) {
      const brief = JSON.stringify(e.payload).slice(0, 120);
      console.log(
        `#${e.seq} ${e.ts} ${e.kind} (${e.source})${e.workItemId ? ` [${e.workItemId}]` : ""} ${brief}`,
      );
    }
    await closeDb();
  });

program
  .command("log")
  .description("render the daily worklog projection")
  .argument("[day]", "YYYY-MM-DD", today())
  .option("-w, --write", "write to worklogs directory")
  .action(async (day: string, opts: { write?: boolean }) => {
    await migrate();
    if (opts.write) {
      const path = await writeDay(day);
      console.log(path);
    } else {
      console.log(await renderDay(day));
    }
    await closeDb();
  });

program
  .command("daemon")
  .description("run the resident runtime: http connector, heartbeat, triage loop")
  .action(async () => {
    await migrate();
    const server = startHttp(config.port);
    const stopHeartbeat = startHeartbeat(config.heartbeatIntervalSec);
    const stopTriage = startTriageLoop(5);
    console.log(`hidane daemon up: http :${config.port}, heartbeat ${config.heartbeatIntervalSec}s`);
    const shutdown = async () => {
      stopHeartbeat();
      stopTriage();
      server.close();
      await disposeAgents();
      await closeDb();
      process.exit(0);
    };
    process.on("SIGINT", () => void shutdown());
    process.on("SIGTERM", () => void shutdown());
  });

program.parseAsync().catch(async (err) => {
  console.error(err);
  await closeDb();
  process.exit(1);
});
