import { mkdir } from "node:fs/promises";
import {
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRuntime,
  SessionManager,
  type AgentSession,
  type CreateAgentSessionOptions,
} from "@earendil-works/pi-coding-agent";
import { config, sessionsDir } from "../config.js";

type Thinking = CreateAgentSessionOptions["thinkingLevel"];
type ModelOpt = CreateAgentSessionOptions["model"];

let runtimePromise: Promise<ModelRuntime> | undefined;
function modelRuntime(): Promise<ModelRuntime> {
  runtimePromise ??= ModelRuntime.create();
  return runtimePromise;
}

async function resolveModel(): Promise<ModelOpt> {
  if (!config.piProvider || !config.piModel) return undefined;
  const runtime = await modelRuntime();
  const model = runtime.getModel(config.piProvider, config.piModel);
  if (!model) {
    throw new Error(
      `model not found: ${config.piProvider}/${config.piModel} (check HIDANE_PI_PROVIDER/HIDANE_PI_MODEL)`,
    );
  }
  return model as ModelOpt;
}

interface RoleSessionOptions {
  charter: string;
  cwd: string;
  sessionDir: string;
  thinking: string;
}

/**
 * Reasoning-only role session (Primary / Manager): no tools, no skills,
 * charter appended to the system prompt, session persisted for continuity.
 * Same loop as workers — different scope, charter and permissions.
 */
async function createRoleSession(opts: RoleSessionOptions): Promise<AgentSession> {
  await mkdir(opts.sessionDir, { recursive: true });
  const agentDir = getAgentDir();
  const resourceLoader = new DefaultResourceLoader({
    cwd: opts.cwd,
    agentDir,
    noExtensions: true,
    noSkills: true,
    noPromptTemplates: true,
    noThemes: true,
    noContextFiles: true,
    appendSystemPrompt: [opts.charter],
  });
  // A caller-provided loader is not reloaded by createAgentSession — do it here,
  // otherwise the charter never reaches the system prompt.
  await resourceLoader.reload();
  const model = await resolveModel();
  const { session } = await createAgentSession({
    cwd: opts.cwd,
    agentDir,
    modelRuntime: await modelRuntime(),
    ...(model !== undefined ? { model } : {}),
    thinkingLevel: opts.thinking as NonNullable<Thinking>,
    noTools: "all",
    resourceLoader,
    sessionManager: SessionManager.create(opts.cwd, opts.sessionDir),
  });
  return session;
}

let primarySession: Promise<AgentSession> | undefined;
const managerSessions = new Map<string, Promise<AgentSession>>();

export function getPrimarySession(charter: string): Promise<AgentSession> {
  primarySession ??= (async () => {
    await mkdir(config.home, { recursive: true });
    return createRoleSession({
      charter,
      cwd: config.home,
      sessionDir: sessionsDir(),
      thinking: config.routeThinking,
    });
  })();
  return primarySession;
}

export function getManagerSession(
  workItemId: string,
  workspace: string,
  sessionDir: string,
  charter: string,
): Promise<AgentSession> {
  let existing = managerSessions.get(workItemId);
  if (!existing) {
    existing = createRoleSession({
      charter,
      cwd: workspace,
      sessionDir,
      thinking: config.routeThinking,
    });
    managerSessions.set(workItemId, existing);
  }
  return existing;
}

/** Pull the last assistant text out of a session's message history. */
export function lastAssistantText(messages: readonly unknown[]): string {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i] as { role?: string; content?: unknown } | undefined;
    if (m?.role !== "assistant") continue;
    const c = m.content;
    if (typeof c === "string" && c.trim()) return c;
    if (Array.isArray(c)) {
      const text = c
        .filter((p) => (p as { type?: string }).type === "text")
        .map((p) => (p as { text?: string }).text ?? "")
        .join("");
      if (text.trim()) return text;
    }
  }
  return "";
}

const promptQueues = new WeakMap<AgentSession, Promise<unknown>>();

/**
 * Serialized, timeout-guarded prompt. Sessions are single-streams; concurrent
 * callers (chat fast lane + triage wake) queue behind each other.
 */
export async function promptRole(
  session: AgentSession,
  text: string,
  timeoutSec: number,
): Promise<{ ok: boolean; text: string; error?: string; durationMs: number }> {
  const prev = promptQueues.get(session) ?? Promise.resolve();
  const run = prev
    .catch(() => {})
    .then(async () => {
      const started = Date.now();
      let timer: NodeJS.Timeout | undefined;
      try {
        await Promise.race([
          session.prompt(text),
          new Promise<never>((_, reject) => {
            timer = setTimeout(() => {
              void session.abort();
              reject(new Error(`role prompt timed out after ${timeoutSec}s`));
            }, timeoutSec * 1000);
          }),
        ]);
        const reply = lastAssistantText(session.messages);
        return { ok: true, text: reply, durationMs: Date.now() - started };
      } catch (err) {
        return {
          ok: false,
          text: "",
          error: String(err instanceof Error ? err.message : err),
          durationMs: Date.now() - started,
        };
      } finally {
        clearTimeout(timer);
      }
    });
  promptQueues.set(session, run);
  return run;
}

/** Dispose all live sessions (daemon shutdown / one-shot CLI exit). */
export async function disposeAgents(): Promise<void> {
  const sessions: Promise<AgentSession>[] = [];
  if (primarySession) sessions.push(primarySession);
  sessions.push(...managerSessions.values());
  primarySession = undefined;
  managerSessions.clear();
  for (const p of sessions) {
    try {
      (await p).dispose();
    } catch {
      // best-effort cleanup
    }
  }
}
