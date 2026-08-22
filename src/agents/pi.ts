import { mkdir } from "node:fs/promises";
import { execa } from "execa";
import { config, sessionsDir } from "../config.js";

export interface RunPiOptions {
  prompt: string;
  /** Charter appended to the system prompt; encodes role scope. */
  charter?: string | undefined;
  /** Working directory; for workers this is the work item's workspace. */
  cwd?: string | undefined;
  /** Enable tools (workers yes, routing/planning no). */
  tools?: boolean | undefined;
  /** Enable native skill discovery (workers yes by default). */
  skills?: boolean | undefined;
  thinking?: string | undefined;
  timeoutSec?: number | undefined;
  /** Directory for pi session traces (execution archive). */
  sessionDir?: string | undefined;
}

export interface PiResult {
  ok: boolean;
  text: string;
  error?: string | undefined;
  durationMs: number;
}

/**
 * Run one non-interactive pi call. All three roles (primary / manager / worker)
 * are the same loop instantiated with different charters, cwd, and permissions.
 */
export async function runPi(opts: RunPiOptions): Promise<PiResult> {
  const sessionDir = opts.sessionDir ?? sessionsDir();
  await mkdir(sessionDir, { recursive: true });
  const args: string[] = ["-p", "--no-extensions", "--session-dir", sessionDir];
  if (opts.tools !== true) args.push("--no-tools");
  if (opts.skills !== true) args.push("--no-skills");
  args.push("--thinking", opts.thinking ?? config.routeThinking);
  if (config.piProvider) args.push("--provider", config.piProvider);
  if (config.piModel) args.push("--model", config.piModel);
  if (opts.charter) args.push("--append-system-prompt", opts.charter);
  args.push(opts.prompt);

  const started = Date.now();
  const result = await execa("pi", args, {
    cwd: opts.cwd ?? process.cwd(),
    timeout: (opts.timeoutSec ?? config.routeTimeoutSec) * 1000,
    reject: false,
    stripFinalNewline: true,
    stdin: "ignore",
    env: { PI_OFFLINE: "1" },
  });
  const durationMs = Date.now() - started;

  if (result.failed || result.exitCode !== 0) {
    const error = result.timedOut
      ? `pi timed out after ${opts.timeoutSec ?? config.routeTimeoutSec}s`
      : (result.stderr || result.stdout || String(result.exitCode)).slice(0, 2000);
    return { ok: false, text: result.stdout ?? "", error, durationMs };
  }
  return { ok: true, text: result.stdout ?? "", durationMs };
}

/** Extract the first JSON object from model output (fenced block preferred). */
export function extractJson<T = Record<string, unknown>>(text: string): T | null {
  const fenced = text.match(/```(?:json)?\s*(\{[\s\S]*?\})\s*```/);
  const candidates: string[] = [];
  if (fenced?.[1]) candidates.push(fenced[1]);
  const start = text.indexOf("{");
  if (start >= 0) {
    let depth = 0;
    let inStr = false;
    let esc = false;
    for (let i = start; i < text.length; i++) {
      const ch = text[i];
      if (esc) {
        esc = false;
        continue;
      }
      if (ch === "\\") {
        esc = true;
        continue;
      }
      if (ch === '"') inStr = !inStr;
      if (inStr) continue;
      if (ch === "{") depth++;
      if (ch === "}") {
        depth--;
        if (depth === 0) {
          candidates.push(text.slice(start, i + 1));
          break;
        }
      }
    }
  }
  for (const c of candidates) {
    try {
      return JSON.parse(c) as T;
    } catch {
      // try next candidate
    }
  }
  return null;
}
