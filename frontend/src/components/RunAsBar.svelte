<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Bot } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type AgentKind, type Effort, type RunAs } from "../lib/api.js";
  import { AGENT_LABELS, effortOptions, withAgent } from "../lib/runAs.js";
  import { AGENT_KINDS, compatibility } from "../lib/settings.js";
  import Combobox, { type ComboboxSuggestion } from "./ui/Combobox.svelte";
  import Select, { type SelectOption } from "./ui/Select.svelte";

  let {
    value,
    onchange,
    scope,
  }: {
    /** `null` follows the role settings. */
    value: RunAs | null;
    onchange: (next: RunAs | null) => void;
    /** `new`: the next task this composer creates; `task`: the addressed task, changed at once. */
    scope: "new" | "task";
  } = $props();

  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings(), staleTime: 60_000 }));
  const agentsQuery = createQuery(() => ({ queryKey: ["agents"], queryFn: () => api.agents(), staleTime: 60_000 }));
  const catalogQuery = createQuery(() => ({
    queryKey: ["agent-models", value?.agent ?? ""],
    queryFn: () => api.agentModels(value?.agent ?? "claude"),
    enabled: value !== null,
    staleTime: 5 * 60_000,
  }));

  const effortLabel = (effort: Effort): string => $t(`settings.effort.${effort || "default"}`);

  /** The CLI the settings would run the work on; the rest is in Settings. */
  let followLabel = $derived.by(() => {
    const worker = settingsQuery.data?.roles.worker;
    return worker ? AGENT_LABELS[worker.agent] : "";
  });
  let available = $derived(new Map((agentsQuery.data?.agents ?? []).map((agent) => [agent.kind, agent.available])));
  let providers = $derived((settingsQuery.data?.providers ?? []).filter((provider) => value !== null && compatibility(value.agent, provider) === null));
  let models = $derived.by((): ComboboxSuggestion[] => {
    if (value === null) return [];
    if (value.provider !== "") return (settingsQuery.data?.providers.find((p) => p.id === value.provider)?.models ?? []).map((id) => ({ value: id }));
    return (catalogQuery.data?.models ?? []).map((model) => ({ value: model.id, label: model.label }));
  });
  let efforts = $derived(value === null ? [] : effortOptions(value, value.provider === "" ? catalogQuery.data : undefined));
  let agentOptions = $derived<SelectOption[]>([
    { value: "", label: followLabel ? $t("runAs.follow", { summary: followLabel }) : $t("runAs.followShort") },
    ...AGENT_KINDS.map((agent) => ({
      value: agent,
      label: available.get(agent) === false ? $t("runAs.unavailable", { agent: AGENT_LABELS[agent] }) : AGENT_LABELS[agent],
      disabled: available.get(agent) === false && value?.agent !== agent,
    })),
  ]);
  let providerOptions = $derived<SelectOption[]>([
    { value: "", label: $t("settings.ownLogin") },
    ...providers.map((provider) => ({ value: provider.id, label: provider.label })),
  ]);
  let effortSelectOptions = $derived<SelectOption[]>(efforts.map((effort) => ({ value: effort, label: effortLabel(effort) })));

  function set(patch: Partial<RunAs>): void {
    if (value === null) return;
    onchange({ ...value, ...patch });
  }

  function commitModel(next: string): void {
    if (value !== null && next !== value.model) set({ model: next });
  }
</script>

<div class="flex flex-wrap items-center gap-1.5 text-xs" role="group" aria-label={$t("runAs.label")}>
  <Bot size={14} class="shrink-0 text-muted" aria-hidden="true" />
  <Select
    class="w-auto max-w-56"
    size="sm"
    label={$t("runAs.agent")}
    value={value?.agent ?? ""}
    options={agentOptions}
    onchange={(next) => onchange(withAgent(next as AgentKind | ""))}
  />
  {#if value !== null}
    {#if providers.length > 0 || value.provider !== ""}
      <Select class="w-auto max-w-40" size="sm" label={$t("runAs.provider")} value={value.provider} options={providerOptions} onchange={(next) => set({ provider: next, model: "" })} />
    {/if}
    <Combobox class="w-44 font-mono" size="sm" label={$t("runAs.model")} emptyLabel={$t("runAs.defaultModel")} value={value.model} suggestions={models} onchange={commitModel} />
    <Select class="w-auto" size="sm" label={$t("runAs.effort")} value={value.effort} options={effortSelectOptions} onchange={(next) => set({ effort: next as Effort })} />
  {/if}
  <span class="text-muted">{$t(scope === "task" ? "runAs.scopeTask" : "runAs.scopeNew")}</span>
  {#if value !== null && value.provider === "" && catalogQuery.data?.error}
    <span class="basis-full text-muted">{$t("runAs.catalogFailed", { agent: AGENT_LABELS[value.agent] })}</span>
  {/if}
</div>
