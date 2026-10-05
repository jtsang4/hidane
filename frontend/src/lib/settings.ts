import i18n from "../i18n/index.js";
import { ApiError } from "./api.js";
import type {
  AgentKind,
  Effort,
  ProviderInput,
  ProviderPatch,
  ProviderPreset,
  ProviderView,
  Role,
  RoleConfig,
} from "./api.js";

export const ROLES: readonly Role[] = ["primary", "manager", "worker", "distiller"];
export const AGENT_KINDS: readonly AgentKind[] = ["claude", "codex", "pi"];
/** Reasoning efforts each CLI accepts (`""` = its default); mirrors settings.EffortsFor. */
export function effortsFor(agent: AgentKind): readonly Effort[] {
  switch (agent) {
    case "claude":
      return ["", "low", "medium", "high", "xhigh", "max"];
    case "codex":
      return ["", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"];
    case "pi":
      return ["", "off", "minimal", "low", "medium", "high", "xhigh", "max"];
  }
}

/** The endpoint fields that decide which CLIs a provider can drive. */
export type ProviderEndpoints = Pick<ProviderView, "anthropicBaseUrl" | "openaiBaseUrl" | "piProvider">;

export type CompatibilityIssue =
  | "settings.compat.claude"
  | "settings.compat.codex"
  | "settings.compat.pi"
  | "settings.compat.piModel"
  | "settings.compat.unknownProvider";

/**
 * Each CLI can only be pointed at a provider through the one wire format it
 * speaks; the server rejects the same combinations with a 400, this only lets
 * the page say so before the round trip. `""` injects nothing, so it always fits.
 */
export function compatibility(agent: AgentKind, provider: ProviderEndpoints | ""): CompatibilityIssue | null {
  if (provider === "") return null;
  if (agent === "claude" && !provider.anthropicBaseUrl.trim()) return "settings.compat.claude";
  if (agent === "codex" && !provider.openaiBaseUrl.trim()) return "settings.compat.codex";
  if (agent === "pi" && !provider.piProvider.trim()) return "settings.compat.pi";
  return null;
}

/** Checks a role or task choice against the saved providers and the CLI's model requirements. */
export function roleCompatibility(
  config: Pick<RoleConfig, "agent" | "provider" | "model">,
  providers: readonly ProviderView[],
): CompatibilityIssue | null {
  if (config.provider === "") return null;
  const provider = providers.find((p) => p.id === config.provider);
  if (!provider) return "settings.compat.unknownProvider";
  const issue = compatibility(config.agent, provider);
  if (issue) return issue;
  if (config.agent === "pi" && !config.model.trim()) return "settings.compat.piModel";
  return null;
}

/** Roles whose saved configuration points at a provider — the reason a delete is refused. */
export function rolesUsingProvider(roles: Partial<Record<Role, Pick<RoleConfig, "provider">>>, providerId: string): Role[] {
  return ROLES.filter((role) => roles[role]?.provider === providerId);
}

/** Editable form state for a provider. `models` stays raw text until submit. */
export interface ProviderDraft {
  id: string;
  label: string;
  anthropicBaseUrl: string;
  openaiBaseUrl: string;
  piProvider: string;
  apiKey: string;
  /** Edit mode only: send `apiKey: ""` so the stored key is removed. */
  clearKey: boolean;
  models: string;
  docsUrl: string;
}

export function emptyDraft(): ProviderDraft {
  return {
    id: "",
    label: "",
    anthropicBaseUrl: "",
    openaiBaseUrl: "",
    piProvider: "",
    apiKey: "",
    clearKey: false,
    models: "",
    docsUrl: "",
  };
}

/**
 * A preset is only a prefill. Its id is suggested as the provider id, suffixed
 * when that id is taken so a second DeepSeek account does not collide.
 */
export function draftFromPreset(preset: ProviderPreset, existingIds: readonly string[] = []): ProviderDraft {
  const taken = new Set(existingIds);
  let id = preset.id;
  for (let n = 2; taken.has(id); n += 1) id = `${preset.id}-${n}`;
  return {
    ...emptyDraft(),
    id,
    label: preset.label,
    anthropicBaseUrl: preset.anthropicBaseUrl,
    openaiBaseUrl: preset.openaiBaseUrl,
    piProvider: preset.piProvider,
    models: formatModels(preset.models),
    docsUrl: preset.docsUrl,
  };
}

/** The key is never sent back, so an edit starts with an empty key field. */
export function draftFromProvider(provider: ProviderView): ProviderDraft {
  return {
    ...emptyDraft(),
    id: provider.id,
    label: provider.label,
    anthropicBaseUrl: provider.anthropicBaseUrl,
    openaiBaseUrl: provider.openaiBaseUrl,
    piProvider: provider.piProvider,
    models: formatModels(provider.models),
  };
}

/** Comma- or newline-separated, trimmed, de-duplicated, order kept. */
export function parseModels(text: string): string[] {
  const seen = new Set<string>();
  for (const part of text.split(/[,\n]/)) {
    const model = part.trim();
    if (model) seen.add(model);
  }
  return [...seen];
}

function formatModels(models: readonly string[]): string {
  return models.join("\n");
}

export type DraftProblem = "settings.providers.needLabel" | "settings.providers.needEndpoint";

export function draftProblem(draft: ProviderDraft): DraftProblem | null {
  if (!draft.label.trim()) return "settings.providers.needLabel";
  if (!draft.anthropicBaseUrl.trim() && !draft.openaiBaseUrl.trim() && !draft.piProvider.trim()) {
    return "settings.providers.needEndpoint";
  }
  return null;
}

function endpoints(draft: ProviderDraft): Omit<ProviderInput, "id" | "apiKey"> {
  return {
    label: draft.label.trim(),
    anthropicBaseUrl: draft.anthropicBaseUrl.trim(),
    openaiBaseUrl: draft.openaiBaseUrl.trim(),
    piProvider: draft.piProvider.trim(),
    models: parseModels(draft.models),
  };
}

export function providerInput(draft: ProviderDraft): ProviderInput {
  const id = draft.id.trim();
  return { ...(id ? { id } : {}), ...endpoints(draft), apiKey: draft.apiKey.trim() };
}

/** An empty key field means "keep"; only the explicit clear action sends `""`. */
export function providerPatch(draft: ProviderDraft): ProviderPatch {
  const apiKey = draft.apiKey.trim();
  return {
    ...endpoints(draft),
    ...(draft.clearKey ? { apiKey: "" } : apiKey ? { apiKey } : {}),
  };
}

/**
 * The server's `{ ok: false, error }` body is the message worth showing; a
 * bare `ApiError` message is `"<status> <body>"`.
 */
export function errorText(error: unknown): string {
  if (!(error instanceof ApiError)) return error instanceof Error ? error.message : String(error);
  if (isDesktopOnly(error)) return i18n.t("common.desktopOnly");
  const body = error.message.replace(/^\d+\s*/, "");
  const message = serverError(error);
  return message || body || error.message;
}

/**
 * A host action (Finder, clipboard, notifications) refused because this is
 * not the desktop app — an explanation of what the browser cannot do, not a
 * failure, so it is not shown as one.
 */
export function isDesktopOnly(error: unknown): boolean {
  // Must match api.DesktopOnly.
  return error instanceof ApiError && serverError(error) === "desktop app only";
}

function serverError(error: ApiError): string {
  try {
    const parsed = JSON.parse(error.message.replace(/^\d+\s*/, "")) as unknown;
    if (typeof parsed === "object" && parsed !== null) {
      const message = (parsed as { error?: unknown }).error;
      if (typeof message === "string") return message;
    }
  } catch {
    // Not JSON: the raw text is the best we have.
  }
  return "";
}
