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
  timeoutSec?: number | undefined;
  /** Called on tool execution boundaries — the two-phase side-effect hook. */
  onToolEvent?: ((e: WorkerToolEvent) => void | Promise<void>) | undefined;
}

export interface WorkerRunResult {
  ok: boolean;
  text: string;
  error?: string | undefined;
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

  try {
    await client.start();
    const unsubscribe = client.onEvent(offEvent);
    await client.prompt(opts.instructions);
    await client.waitForIdle(timeoutSec * 1000);
    unsubscribe();
    const text = lastAssistantText(finalMessages);
    return { ok: true, text, durationMs: Date.now() - started, toolCalls };
  } catch (err) {
    const stderr = client.getStderr().slice(0, 1000);
    return {
      ok: false,
      text: lastAssistantText(finalMessages),
      error: `${String(err instanceof Error ? err.message : err)}${stderr ? `\nstderr: ${stderr}` : ""}`,
      durationMs: Date.now() - started,
      toolCalls,
    };
  } finally {
    await client.stop().catch(() => {});
  }
}
