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
  runtimePromise ??= createModelRuntime().then(async (runtime) => {
    // Held in memory only (pi's "runtime" credential source): never written to
    // auth.json, so the environment stays the one place a key comes from.
    if (config.piApiKey && config.piProvider) {
      await runtime.setRuntimeApiKey(config.piProvider, config.piApiKey);
    }
    return runtime;
  });
  return runtimePromise;
}

/**
 * Keep the baked-in project catalog and pi's remote catalog in sync. This is
 * the SDK equivalent of `pi update --models`: restore the local cache first,
 * then force a bounded network refresh so configured aliases remain usable on
 * a fresh deployment as well as after a provider adds a model.
 */
async function createModelRuntime(): Promise<ModelRuntime> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15_000);
  try {
    const runtime = await ModelRuntime.create({
      allowModelNetwork: false,
      signal: controller.signal,
    });

    if (process.env.PI_OFFLINE === undefined) {
      try {
        const result = await runtime.refresh({
          allowNetwork: true,
          force: true,
          signal: controller.signal,
        });
        if (result.aborted) {
          console.warn("pi model catalog refresh timed out; using the local catalog");
        } else if (result.errors.size > 0) {
          const details = [...result.errors]
            .map(([provider, error]) => `${provider}: ${error.message}`)
            .join("; ");
          console.warn(`pi model catalog refresh failed; using the local catalog (${details})`);
        } else {
          console.log("pi model catalog refreshed");
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        console.warn(`pi model catalog refresh failed; using the local catalog (${message})`);
      }
    }

    return runtime;
  } finally {
    clearTimeout(timeout);
  }
}

/**
 * Resolve the configured model. A half-configured pair (one of provider/model
 * set) is an error rather than a silent fallback to pi's own default — that
 * fallback once had production quietly running a different model than intended.
 */
async function resolveModel(): Promise<ModelOpt> {
  const { piProvider, piModel, piApiKey } = config;
  if (piApiKey && !piProvider) {
    throw new Error("HIDANE_PI_API_KEY is set but HIDANE_PI_PROVIDER is not: a key needs to know which provider it is for");
  }
  if (!piProvider && !piModel) {
    await modelRuntime();
    return undefined;
  }
  if (!piProvider || !piModel) {
    throw new Error(
      `HIDANE_PI_PROVIDER and HIDANE_PI_MODEL must be set together (got provider=${piProvider ?? "unset"}, model=${piModel ?? "unset"})`,
    );
  }
  const runtime = await modelRuntime();
  const model = runtime.getModel(piProvider, piModel);
  if (!model) {
    const known = runtime.getModels(piProvider).map((m) => m.id);
    throw new Error(
      known.length > 0
        ? `model not found: ${piProvider}/${piModel}. ${piProvider} offers: ${known.join(", ")}`
        : `unknown provider: ${piProvider} (check HIDANE_PI_PROVIDER against the pi model catalog)`,
    );
  }
  // Checked here rather than on the first message: a missing key otherwise
  // surfaces as every reply failing, long after the deploy looked healthy.
  if (!runtime.getProviderAuthStatus(piProvider).configured) {
    throw new Error(`no API key for provider ${piProvider}: set HIDANE_PI_API_KEY`);
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
    const m = resolved as unknown as { provider?: string; id?: string; input?: string[] };
    const provider = m.provider ?? config.piProvider ?? "";
    const auth = (await modelRuntime()).getProviderAuthStatus(provider);
    // Where the key came from, never the key: "runtime" is HIDANE_PI_API_KEY.
    const keySource = auth.source === "runtime" ? "HIDANE_PI_API_KEY" : (auth.label ?? auth.source ?? "unknown");
    const images = m.input?.includes("image") ? "" : ", no image input";
    return `${provider}/${m.id ?? config.piModel} (configured, key from ${keySource}${images})`;
  }
  return "pi default (HIDANE_PI_PROVIDER/HIDANE_PI_MODEL unset)";
}

interface RoleSessionOptions {
  charter: string;
  cwd: string;
  sessionDir: string;
  thinking: string;
  /** Leave no trace file (a health check is not agent work worth keeping). */
  ephemeral?: boolean;
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
    sessionManager: opts.ephemeral ? SessionManager.inMemory(opts.cwd) : SessionManager.create(opts.cwd, opts.sessionDir),
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

/**
 * A fresh Primary session for one turn. The Primary's continuity is rebuilt
 * from the log each turn (`recentConversation`), so nothing carries over in
 * the session: it cannot grow without bound, needs no compaction mid-turn,
 * and behaves the same before and after a restart. Each turn still leaves its
 * own trace file. The caller disposes it.
 */
export async function openPrimarySession(charter: string): Promise<AgentSession> {
  await mkdir(config.home, { recursive: true });
  return createRoleSession({
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

/** The provider's error, when the last assistant message ended in one. */
export function lastAssistantError(messages: readonly unknown[]): string | undefined {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i] as { role?: string; stopReason?: string; errorMessage?: string } | undefined;
    if (m?.role !== "assistant") continue;
    return m.stopReason === "error" ? (m.errorMessage ?? "model request failed") : undefined;
  }
  return undefined;
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
  /**
   * Raw assistant text as it is produced. Charters make that text JSON, so a
   * caller wanting to show it to a human must extract the human-facing field
   * first — see `createReplyExtractor`.
   */
  onDelta?: (delta: string) => void,
): Promise<{ ok: boolean; text: string; error?: string; durationMs: number }> {
  const prev = promptQueues.get(session) ?? Promise.resolve();
  const run = prev
    .catch(() => {})
    .then(async () => {
      const started = Date.now();
      let timer: NodeJS.Timeout | undefined;
      // Safe despite the shared session: prompts on it are serialized by this
      // very queue, so the subscription can only observe our own turn.
      let unsubscribe: (() => void) | undefined;
      try {
        if (onDelta) {
          unsubscribe = session.subscribe((event) => {
            if (event.type !== "message_update") return;
            const update = event.assistantMessageEvent;
            // Thinking deltas are reasoning, not an answer; they never surface.
            if (update.type !== "text_delta") return;
            onDelta(update.delta);
          });
        }
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
        // A provider failure (bad key, unknown model, quota) does not throw: it
        // ends the turn with an error message, which would otherwise read as
        // an empty answer and hide the reason.
        const failure = lastAssistantError(session.messages);
        if (failure) {
          return { ok: false, text: "", error: failure, durationMs: Date.now() - started };
        }
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
        unsubscribe?.();
      }
    });
  promptQueues.set(session, run);
  return run;
}

/**
 * One real round trip to the configured model — the only proof that a
 * provider, model and key actually work together, short of a failed reply.
 */
export async function pingModel(timeoutSec = 90): Promise<{ ok: boolean; text: string; error?: string; durationMs: number }> {
  await mkdir(config.home, { recursive: true });
  const session = await createRoleSession({
    charter: "This is a connectivity check. Reply with exactly: OK",
    cwd: config.home,
    sessionDir: sessionsDir(),
    thinking: "minimal",
    ephemeral: true,
  });
  try {
    return await promptRole(session, "ping", timeoutSec);
  } finally {
    session.dispose();
  }
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
