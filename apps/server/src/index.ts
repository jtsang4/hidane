#!/usr/bin/env node
import { Command } from "commander";
import { config } from "./config.js";
import { migrate, closeDb } from "./kernel/db.js";
import { listEvents, type HidaneEvent } from "./kernel/events.js";
import { listWorkItems } from "./kernel/workItems.js";
import { activeExecutions } from "./kernel/executions.js";
import { mailboxesWithPending } from "./kernel/mailbox.js";
import { onEventAppended } from "./kernel/notify.js";
import { acquireRuntimeLock } from "./kernel/runtime.js";
import { submitMessage } from "./agents/ingress.js";
import { createAgentRuntime, setCurrentRuntime } from "./agents/runtime.js";
import { recoverExecutions } from "./agents/workerPool.js";
import { disposeAgents, describeEffectiveModel, pingModel } from "./agents/sdk.js";
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
import { startScheduler } from "./connectors/scheduler.js";
import { startFeishuOutbox } from "./connectors/feishu.js";
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

/** Print answers to one message until its chain has gone quiet. */
async function followAnswers(messageId: string, timeoutMs: number): Promise<void> {
  const seen = new Set<string>();
  const started = Date.now();
  let lastActivity = Date.now();
  let wake: (() => void) | undefined;
  const unlisten = onEventAppended(() => wake?.());
  try {
    while (Date.now() - started < timeoutMs) {
      const events = await listEvents({ conversation: true, tail: 200 });
      const mine = events.filter(
        (e: HidaneEvent) => e.payload["root"] === messageId || e.payload["of"] === messageId,
      );
      for (const e of mine) {
        if (seen.has(e.id)) continue;
        seen.add(e.id);
        lastActivity = Date.now();
        const text = e.payload["text"] ?? e.payload["question"] ?? e.payload["error"];
        const label = e.kind === "message.attributed" ? `→ ${String(e.payload["workItemId"])}` : e.kind;
        console.log(`\n[${label}${e.workItemId && e.kind !== "message.attributed" ? ` ${e.workItemId}` : ""}]`);
        if (typeof text === "string") console.log(text);
      }
      const answered = mine.some((e) => ["agent.reply", "escalation", "attribution.ambiguous", "agent.error"].includes(e.kind));
      const busy = (await activeExecutions()).length > 0 || (await mailboxesWithPending()).length > 0;
      if (answered && !busy && Date.now() - lastActivity > 1500) return;
      await new Promise<void>((resolve) => {
        const t = setTimeout(resolve, 2000);
        wake = () => {
          clearTimeout(t);
          resolve();
        };
      });
    }
    console.log("\n(still working — follow it in the web UI or with `hidane events`)");
  } finally {
    unlisten();
  }
}

program
  .command("chat")
  .description("send a message to the Primary agent and follow the answers")
  .argument("<message...>", "message text")
  .option("--item <id>", "address the message to a work item directly")
  .option("--timeout <sec>", "stop following after this many seconds", "900")
  .action(async (parts: string[], opts: { item?: string; timeout: string }) => {
    await migrate();
    // Without a daemon, this process runs the loop itself for as long as the
    // conversation is active; with one, it only posts and follows.
    const release = await acquireRuntimeLock();
    const runtime = release ? createAgentRuntime() : undefined;
    if (runtime) {
      await recoverExecutions();
      setCurrentRuntime(runtime);
      runtime.start();
    }
    const message = await submitMessage({
      text: parts.join(" "),
      source: "connector:cli",
      ...(opts.item ? { target: opts.item } : {}),
    });
    await followAnswers(message.id, Number(opts.timeout) * 1000);
    await runtime?.stop();
    await release?.();
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
  .command("model")
  .description("show the provider, model and key source agents will use; --ping calls the model once")
  .option("--ping", "make one real request to verify the key and model work")
  .action(async (opts: { ping?: boolean }) => {
    try {
      console.log(`model: ${await describeEffectiveModel()}`);
      if (opts.ping) {
        const result = await pingModel();
        if (!result.ok || !result.text.trim()) {
          console.error(`ping failed after ${result.durationMs}ms: ${result.error ?? "empty reply"}`);
          process.exit(1);
        }
        console.log(`ping ok in ${result.durationMs}ms: ${result.text.trim().slice(0, 80)}`);
      }
      process.exit(0);
    } catch (err) {
      console.error(err instanceof Error ? err.message : String(err));
      process.exit(1);
    }
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
  .description("run the resident runtime: agent event loop, http, connectors")
  .action(async () => {
    const effectiveModel = await describeEffectiveModel();
    await migrate();
    const release = await acquireRuntimeLock();
    if (!release) {
      console.error("another hidane runtime holds the lock on this database; refusing to start a second one");
      await closeDb();
      process.exit(1);
    }
    const lost = await recoverExecutions();
    const runtime = createAgentRuntime();
    setCurrentRuntime(runtime);
    runtime.start();
    const server = startHttp(config.port);
    const stopHeartbeat = startHeartbeat(config.heartbeatIntervalSec);
    const stopTriage = startTriageLoop(5);
    const stopScheduler = startScheduler(5);
    const stopOutbox = startFeishuOutbox();
    console.log(
      `hidane daemon up: http :${config.port}, heartbeat ${config.heartbeatIntervalSec}s, workers ${config.maxWorkers}, turns ${config.maxConcurrentTurns}${lost > 0 ? `, ${lost} execution(s) lost to the last restart` : ""}`,
    );
    console.log(`model: ${effectiveModel}`);
    const shutdown = async () => {
      stopHeartbeat();
      stopTriage();
      stopScheduler();
      stopOutbox();
      server.close();
      await runtime.stop();
      await release();
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
