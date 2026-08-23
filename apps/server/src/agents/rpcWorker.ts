import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { mkdir } from "node:fs/promises";
import {
  RpcClient,
  type JsonAgentSessionEvent,
} from "@earendil-works/pi-coding-agent";
import { config } from "../config.js";
import { lastAssistantText } from "./sdk.js";

function cliPath(): string {
  // The package is ESM-only (exports has no CJS entry), so resolve the main
  // entry (dist/index.js) via import.meta.resolve and take its sibling cli.js.
  const entry = fileURLToPath(
    import.meta.resolve("@earendil-works/pi-coding-agent"),
  );
  return join(dirname(entry), "cli.js");
}

function guardExtensionPath(): string {
  // Lives outside src/ (pi loads it with its own TS loader); same relative
  // location from src/agents and dist/agents.
  return fileURLToPath(new URL("../../extensions/pi-guard.ts", import.meta.url));
}

interface Controllable {
  steer(message: string): Promise<void>;
  abort(): Promise<void>;
}

interface ActiveEntry {
  client?: Controllable | undefined;
  /** Steers arriving before the RPC client is up are buffered, then flushed. */
  buffer: string[];
  /** Set by cancelActiveWorker so the failure reads as intent, not a crash. */
  cancelled?: boolean;
  /** Resolved on cancel; the run races it so a stop always terminates. */
  onCancel?: (() => void) | undefined;
  executionId?: string | undefined;
}

/** Running executions by work item — the "唯一在跑的 Execution" routing target. */
const activeWorkers = new Map<string, ActiveEntry>();

export function hasActiveWorker(workItemId: string): boolean {
  return activeWorkers.has(workItemId);
}

/** Execution id currently running for this work item, if any. */
export function activeExecutionId(workItemId: string): string | undefined {
  return activeWorkers.get(workItemId)?.executionId;
}

/**
 * Stop a running execution.
 *
 * Executions legitimately run for minutes, so a wrong one previously had to be
 * waited out to the 600s timeout — the user could only watch.
 *
 * Two things are needed, and the first alone is not enough: `abort()` asks the
 * agent to interrupt, but `waitForIdle` does not settle just because the run
 * was aborted, so a cancel reported success while the execution hung on with
 * no terminal event (measured: still running 90s after an accepted cancel).
 * The run therefore also races an explicit cancellation signal.
 */
export async function cancelActiveWorker(workItemId: string): Promise<boolean> {
  const entry = activeWorkers.get(workItemId);
  if (!entry?.client) return false;
  entry.cancelled = true;
  await entry.client.abort().catch(() => {});
  entry.onCancel?.();
  return true;
}

/** Inject a message into the running (or starting) execution for this work item. */
export async function steerActiveWorker(
  workItemId: string,
  text: string,
): Promise<boolean> {
  const entry = activeWorkers.get(workItemId);
  if (!entry) return false;
  if (!entry.client) {
    entry.buffer.push(text);
    return true;
  }
  try {
    await entry.client.steer(text);
    return true;
  } catch {
    return false;
  }
}

export interface WorkerToolEvent {
  phase: "start" | "end";
  toolName: string;
  isError?: boolean | undefined;
  detail?: string | undefined;
}

export interface WorkerRunOptions {
  instructions: string;
  cwd: string;
  charter: string;
  sessionDir: string;
  /** Registers the execution as steerable for this work item while it runs. */
  workItemId?: string | undefined;
  /** Reported back so the UI can name what it is offering to cancel. */
  executionId?: string | undefined;
  timeoutSec?: number | undefined;
  /** Called on tool execution boundaries — the two-phase side-effect hook. */
  onToolEvent?: ((e: WorkerToolEvent) => void | Promise<void>) | undefined;
}

export interface WorkerRunResult {
  ok: boolean;
  text: string;
  error?: string | undefined;
  /** Distinguishes a deliberate stop from a failure, for the log and the UI. */
  cancelled?: boolean | undefined;
  durationMs: number;
  toolCalls: number;
}

/**
 * One worker execution = one isolated `pi --mode rpc` subprocess in the work
 * item's workspace (own cwd, crash isolation), with native skill discovery on
 * and the full event stream captured back into hidane's world.
 */
export async function runWorkerExecution(
  opts: WorkerRunOptions,
): Promise<WorkerRunResult> {
  await mkdir(opts.sessionDir, { recursive: true });
  const timeoutSec = opts.timeoutSec ?? config.workerTimeoutSec;
  const args = [
    "--no-extensions",
    "-e",
    guardExtensionPath(),
    "--thinking",
    config.workerThinking,
    "--session-dir",
    opts.sessionDir,
    "--append-system-prompt",
    opts.charter,
  ];
  const client = new RpcClient({
    cliPath: cliPath(),
    cwd: opts.cwd,
    env: { ...(process.env as Record<string, string>), PI_OFFLINE: "1" },
    ...(config.piProvider ? { provider: config.piProvider } : {}),
    ...(config.piModel ? { model: config.piModel } : {}),
    args,
  });

  const started = Date.now();
  let toolCalls = 0;
  const finalMessages: unknown[] = [];

  const offEvent = (event: JsonAgentSessionEvent): void => {
    const e = event as Record<string, unknown>;
    switch (e["type"]) {
      case "tool_execution_start": {
        toolCalls++;
        void opts.onToolEvent?.({
          phase: "start",
          toolName: String(e["toolName"] ?? "unknown"),
          detail: JSON.stringify(e["input"] ?? e["args"] ?? {}).slice(0, 500),
        });
        break;
      }
      case "tool_execution_end": {
        void opts.onToolEvent?.({
          phase: "end",
          toolName: String(e["toolName"] ?? "unknown"),
          isError: Boolean(e["isError"]),
        });
        break;
      }
      case "message_end": {
        if (e["message"]) finalMessages.push(e["message"]);
        break;
      }
      default:
        break;
    }
  };

  const entry: ActiveEntry = { buffer: [], executionId: opts.executionId };
  if (opts.workItemId) activeWorkers.set(opts.workItemId, entry);
  try {
    await client.start();
    entry.client = client;
    const unsubscribe = client.onEvent(offEvent);
    await client.prompt(opts.instructions);
    for (const buffered of entry.buffer.splice(0)) {
      await client.steer(buffered).catch(() => {});
    }
    // Wake on either normal completion or a cancellation.
    const cancelSignal = new Promise<never>((_, reject) => {
      entry.onCancel = () => reject(new Error("cancelled"));
    });
    try {
      await Promise.race([client.waitForIdle(timeoutSec * 1000), cancelSignal]);
    } catch (err) {
      if (!entry.cancelled) throw err;
    }
    unsubscribe();
    const text = lastAssistantText(finalMessages);
    // The outcome follows the user's intent, not which promise won the race:
    // abort() can make the agent go idle first, and reporting that as a clean
    // success made a deliberately stopped run look like a completed one.
    if (entry.cancelled) {
      return {
        ok: false,
        text,
        error: "cancelled",
        cancelled: true,
        durationMs: Date.now() - started,
        toolCalls,
      };
    }
    return { ok: true, text, durationMs: Date.now() - started, toolCalls };
  } catch (err) {
    if (entry.cancelled) {
      return {
        ok: false,
        text: lastAssistantText(finalMessages),
        error: "cancelled",
        cancelled: true,
        durationMs: Date.now() - started,
        toolCalls,
      };
    }
    const stderr = client.getStderr().slice(0, 1000);
    return {
      ok: false,
      text: lastAssistantText(finalMessages),
      error: `${String(err instanceof Error ? err.message : err)}${stderr ? `\nstderr: ${stderr}` : ""}`,
      durationMs: Date.now() - started,
      toolCalls,
    };
  } finally {
    if (opts.workItemId) activeWorkers.delete(opts.workItemId);
    await client.stop().catch(() => {});
  }
}
