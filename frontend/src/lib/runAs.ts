import type { AgentCatalog, AgentKind, Effort, RunAs } from "./api.js";
import { effortsFor } from "./settings.js";

/**
 * Choosing what a task runs on, as Paseo's composer does: an agent CLI, then
 * (optionally) a provider, a model and a reasoning effort. `null` follows the
 * role settings. The choice for new tasks is remembered on this machine; a
 * task's own choice lives on the task.
 */

const STORAGE_KEY = "hidane.runAs";

export const AGENT_LABELS: Record<AgentKind, string> = { claude: "Claude Code", codex: "Codex", pi: "pi" };

export function loadDraftRunAs(storage: Pick<Storage, "getItem"> | undefined = globalThis.localStorage): RunAs | null {
  try {
    const raw = storage?.getItem(STORAGE_KEY);
    if (!raw) return null;
    return normalizeRunAs(JSON.parse(raw) as Partial<RunAs>);
  } catch {
    return null;
  }
}

export function saveDraftRunAs(value: RunAs | null, storage: Pick<Storage, "setItem" | "removeItem"> | undefined = globalThis.localStorage): void {
  try {
    if (value) storage?.setItem(STORAGE_KEY, JSON.stringify(value));
    else storage?.removeItem(STORAGE_KEY);
  } catch {
    // A choice that cannot be remembered still applies to this message.
  }
}

/** A stored or received choice, made safe: unknown agents drop it, an effort the CLI does not take resets. */
export function normalizeRunAs(value: Partial<RunAs> | null | undefined): RunAs | null {
  if (!value || (value.agent !== "claude" && value.agent !== "codex" && value.agent !== "pi")) return null;
  const effort = effortsFor(value.agent).includes(value.effort ?? "") ? (value.effort ?? "") : "";
  return { agent: value.agent, provider: value.provider ?? "", model: (value.model ?? "").trim(), effort };
}

/** Switching agent keeps nothing that belonged to the previous CLI. */
export function withAgent(agent: AgentKind | ""): RunAs | null {
  return agent === "" ? null : { agent, provider: "", model: "", effort: "" };
}

/**
 * The efforts to offer: those the chosen model takes, when the catalog knows
 * it, else every effort the CLI accepts. The current value stays listed so a
 * choice made elsewhere is never silently dropped from view.
 */
export function effortOptions(value: RunAs, catalog: AgentCatalog | undefined): Effort[] {
  const model = value.provider === "" ? catalog?.models.find((m) => m.id === value.model) : undefined;
  const base: Effort[] = model?.efforts && model.efforts.length > 0 ? ["", ...model.efforts] : [...effortsFor(value.agent)];
  if (!base.includes(value.effort)) base.push(value.effort);
  return base;
}

/** "Codex · gpt-5.5 · 高" — the label words come from the caller's translations. */
export function runAsSummary(value: RunAs, effortLabel: (effort: Effort) => string, defaultModel: string): string {
  const parts = [AGENT_LABELS[value.agent], value.model || defaultModel];
  if (value.effort) parts.push(effortLabel(value.effort));
  return parts.join(" · ");
}

export function sameRunAs(a: RunAs | null | undefined, b: RunAs | null | undefined): boolean {
  if (!a || !b) return !a && !b;
  return a.agent === b.agent && a.provider === b.provider && a.model === b.model && a.effort === b.effort;
}
