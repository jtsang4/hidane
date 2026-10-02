<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Bot } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type AgentKind, type Effort, type RunAs } from "../lib/api.js";
  import { AGENT_LABELS, effortOptions, withAgent } from "../lib/runAs.js";
  import { AGENT_KINDS, compatibility } from "../lib/settings.js";

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

  const ids = $props.id();
  const effortLabel = (effort: Effort): string => $t(`settings.effort.${effort || "default"}`);

  /** The CLI the settings would run the work on; the rest is in Settings. */
  let followLabel = $derived.by(() => {
    const worker = settingsQuery.data?.roles.worker;
    return worker ? AGENT_LABELS[worker.agent] : "";
  });
  let available = $derived(new Map((agentsQuery.data?.agents ?? []).map((agent) => [agent.kind, agent.available])));
  let providers = $derived((settingsQuery.data?.providers ?? []).filter((provider) => value !== null && compatibility(value.agent, provider) === null));
  let models = $derived.by(() => {
    if (value === null) return [];
    if (value.provider !== "") return (settingsQuery.data?.providers.find((p) => p.id === value.provider)?.models ?? []).map((id) => ({ id, label: id }));
    return (catalogQuery.data?.models ?? []).map((model) => ({ id: model.id, label: model.label || model.id }));
  });
  let efforts = $derived(value === null ? [] : effortOptions(value, value.provider === "" ? catalogQuery.data : undefined));
  /** What is typed in the model field; follows the saved value until edited. */
  let modelDraft = $derived(value?.model ?? "");

  function set(patch: Partial<RunAs>): void {
    if (value === null) return;
    onchange({ ...value, ...patch });
  }

  function commitModel(): void {
    const model = modelDraft.trim();
    if (value !== null && model !== value.model) set({ model });
  }

  const control = "h-7 rounded-md border border-border bg-surface px-2 text-xs focus-visible:outline-2 focus-visible:outline-primary";
</script>

<div class="flex flex-wrap items-center gap-1.5 text-xs" role="group" aria-label={$t("runAs.label")}>
  <Bot size={14} class="shrink-0 text-muted" aria-hidden="true" />
  <select
    id={`${ids}-agent`}
    class={`${control} max-w-56`}
    aria-label={$t("runAs.agent")}
    value={value?.agent ?? ""}
    onchange={(event) => onchange(withAgent(event.currentTarget.value as AgentKind | ""))}
  >
    <option value="">{followLabel ? $t("runAs.follow", { summary: followLabel }) : $t("runAs.followShort")}</option>
    {#each AGENT_KINDS as agent (agent)}
      <option value={agent} disabled={available.get(agent) === false && value?.agent !== agent}>
        {available.get(agent) === false ? $t("runAs.unavailable", { agent: AGENT_LABELS[agent] }) : AGENT_LABELS[agent]}
      </option>
    {/each}
  </select>
  {#if value !== null}
    {#if providers.length > 0 || value.provider !== ""}
      <select class={`${control} max-w-40`} aria-label={$t("runAs.provider")} value={value.provider} onchange={(event) => set({ provider: event.currentTarget.value, model: "" })}>
        <option value="">{$t("settings.ownLogin")}</option>
        {#each providers as provider (provider.id)}
          <option value={provider.id}>{provider.label}</option>
        {/each}
      </select>
    {/if}
    <input
      class={`${control} w-44 font-mono`}
      list={`${ids}-models`}
      aria-label={$t("runAs.model")}
      placeholder={$t("runAs.defaultModel")}
      bind:value={modelDraft}
      onchange={commitModel}
      onkeydown={(event) => { if (event.key === "Enter" && !event.isComposing) { event.preventDefault(); commitModel(); } }}
    />
    <datalist id={`${ids}-models`}>
      {#each models as model (model.id)}
        <option value={model.id}>{model.label}</option>
      {/each}
    </datalist>
    <select class={control} aria-label={$t("runAs.effort")} value={value.effort} onchange={(event) => set({ effort: event.currentTarget.value as Effort })}>
      {#each efforts as effort (effort)}
        <option value={effort}>{effortLabel(effort)}</option>
      {/each}
    </select>
  {/if}
  <span class="text-muted">{$t(scope === "task" ? "runAs.scopeTask" : "runAs.scopeNew")}</span>
  {#if value !== null && value.provider === "" && catalogQuery.data?.error}
    <span class="basis-full text-muted">{$t("runAs.catalogFailed", { agent: AGENT_LABELS[value.agent] })}</span>
  {/if}
</div>
