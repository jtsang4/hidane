#!/usr/bin/env node
import { Command } from "commander";
import { config } from "./config.js";
import { migrate, closeDb } from "./kernel/db.js";
import { listEvents } from "./kernel/events.js";
import { listWorkItems } from "./kernel/workItems.js";
import { handleUserMessage } from "./agents/primary.js";
import { disposeAgents, describeEffectiveModel } from "./agents/sdk.js";
import { runDistillation } from "./agents/distiller.js";
import {
  forgetMemory,
  globalMemoryPath,
  parseMemories,
  readMemoryFile,
} from "./kernel/memories.js";
import { startHeartbeat } from "./connectors/timer.js";
import { startHttp } from "./connectors/http.js";
import { startTriageLoop } from "./connectors/triageLoop.js";
import { renderDay, writeDay, today } from "./projections/worklog.js";
import { archiveDay } from "./projections/archive.js";

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
  .command("archive")
  .description("archive a day: worklog + session traces into worklogs/YYYY/MM/DD/")
  .argument("[day]", "YYYY-MM-DD", today())
  .action(async (day: string) => {
    await migrate();
    const result = await archiveDay(day);
    console.log(`${result.dir} (${result.sessions} session files)`);
    await closeDb();
  });

program
  .command("distill")
  .description("run one memory distillation pass over new events")
  .option("--min <n>", "minimum meaningful events required", "1")
  .action(async (opts: { min: string }) => {
    await migrate();
    const result = await runDistillation({ minEvents: Number(opts.min) });
    console.log(JSON.stringify(result));
    await disposeAgents();
    await closeDb();
    process.exit(0);
  });

program
  .command("memories")
  .description("print the global memory file")
  .option("--ids", "list entries with their ids")
  .action(async (opts: { ids?: boolean }) => {
    const text = await readMemoryFile(globalMemoryPath());
    if (text.trim() === "") {
      console.log(`(empty) ${globalMemoryPath()}`);
      return;
    }
    if (opts.ids) {
      for (const m of parseMemories(text)) {
        console.log(`${m.id}  [${m.kind}] (${m.date}) ${m.content}`);
      }
      return;
    }
    console.log(text);
  });

program
  .command("forget")
  .description("remove a memory entry by id (memories age out; this is the expiry channel)")
  .argument("<id>", "memory id, e.g. mem_ab12cd")
  .action(async (id: string) => {
    await migrate();
    const ok = await forgetMemory(globalMemoryPath(), id, "cli");
    console.log(ok ? `forgot ${id}` : `not found: ${id}`);
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
    const distillTimer = setInterval(() => {
      runDistillation({ minEvents: 10 }).catch((err) =>
        console.error("distill loop error:", err),
      );
    }, config.distillIntervalSec * 1000);
    const archiveTimer = setInterval(() => {
      archiveDay(today()).catch((err) => console.error("archive loop error:", err));
    }, 3600 * 1000);
    console.log(`hidane daemon up: http :${config.port}, heartbeat ${config.heartbeatIntervalSec}s, distill ${config.distillIntervalSec}s`);
    console.log(`model: ${await describeEffectiveModel()}`);
    const shutdown = async () => {
      stopHeartbeat();
      stopTriage();
      clearInterval(distillTimer);
      clearInterval(archiveTimer);
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
