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

/**
 * Resolve the configured model. A half-configured pair (one of provider/model
 * set) is an error rather than a silent fallback to pi's own default — that
 * fallback once had production quietly running a different model than intended.
 */
async function resolveModel(): Promise<ModelOpt> {
  const { piProvider, piModel } = config;
  if (!piProvider && !piModel) return undefined;
  if (!piProvider || !piModel) {
    throw new Error(
      `HIDANE_PI_PROVIDER and HIDANE_PI_MODEL must be set together (got provider=${piProvider ?? "unset"}, model=${piModel ?? "unset"})`,
    );
  }
  const runtime = await modelRuntime();
  const model = runtime.getModel(piProvider, piModel);
  if (!model) {
    throw new Error(
      `model not found: ${piProvider}/${piModel} (check HIDANE_PI_PROVIDER/HIDANE_PI_MODEL and the pi model catalog)`,
    );
  }
  return model as ModelOpt;
}

/** What model will agents actually use? Printed at daemon start so the answer
 *  never has to be guessed from logs or inferred from behaviour. */
export async function describeEffectiveModel(): Promise<string> {
  const resolved = await resolveModel().catch((err) => {
    throw err instanceof Error ? err : new Error(String(err));
  });
  if (resolved) {
    const m = resolved as unknown as { provider?: string; id?: string };
    return `${m.provider ?? config.piProvider}/${m.id ?? config.piModel} (configured)`;
  }
  return "pi default (HIDANE_PI_PROVIDER/HIDANE_PI_MODEL unset)";
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

const roleSessions = new Map<string, Promise<AgentSession>>();

/** Named session cache: one persistent session per role instance. */
export function getRoleSession(
  name: string,
  opts: RoleSessionOptions,
): Promise<AgentSession> {
  let existing = roleSessions.get(name);
  if (!existing) {
    existing = (async () => {
      await mkdir(opts.cwd, { recursive: true });
      return createRoleSession(opts);
    })();
    roleSessions.set(name, existing);
  }
  return existing;
}

export function getPrimarySession(charter: string): Promise<AgentSession> {
  return getRoleSession("primary", {
    charter,
    cwd: config.home,
    sessionDir: sessionsDir(),
    thinking: config.routeThinking,
  });
}

export function getManagerSession(
  workItemId: string,
  workspace: string,
  sessionDir: string,
  charter: string,
): Promise<AgentSession> {
  return getRoleSession(`manager:${workItemId}`, {
    charter,
    cwd: workspace,
    sessionDir,
    thinking: config.routeThinking,
  });
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
  /** Inbound images (e.g. from Feishu) forwarded to the vision model. */
  images: { data: string; mimeType: string }[] = [],
): Promise<{ ok: boolean; text: string; error?: string; durationMs: number }> {
  const prev = promptQueues.get(session) ?? Promise.resolve();
  const run = prev
    .catch(() => {})
    .then(async () => {
      const started = Date.now();
      let timer: NodeJS.Timeout | undefined;
      try {
        const promptOptions =
          images.length > 0
            ? {
                images: images.map((img) => ({
                  type: "image" as const,
                  data: img.data,
                  mimeType: img.mimeType,
                })),
              }
            : undefined;
        await Promise.race([
          promptOptions ? session.prompt(text, promptOptions) : session.prompt(text),
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
  const sessions = [...roleSessions.values()];
  roleSessions.clear();
  for (const p of sessions) {
    try {
      (await p).dispose();
    } catch {
      // best-effort cleanup
    }
  }
}
