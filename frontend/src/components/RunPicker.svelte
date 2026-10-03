<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Dialog, Popover } from "bits-ui";
  import { MediaQuery } from "svelte/reactivity";
  import { Bot, Check, ChevronsUpDown, MessageCircle, Settings2, Star, TriangleAlert, X } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type AgentKind, type Effort, type RunAs } from "../lib/api.js";
  import { favorites, toggleFavoriteRun } from "../lib/favorites.svelte.js";
  import { openSettings } from "../lib/router.svelte.js";
  import { AGENT_LABELS, effortOptions, favoriteLabel, sameRunAs, withAgent } from "../lib/runAs.js";
  import { AGENT_KINDS, compatibility, roleCompatibility } from "../lib/settings.js";
  import { segment, segmented, segmentOff, segmentOn, sheetHandle } from "../lib/styles.js";
  import { cn } from "../lib/utils.js";
  import Combobox, { type ComboboxSuggestion } from "./ui/Combobox.svelte";
  import Select, { type SelectOption } from "./ui/Select.svelte";

  let {
    kind,
    value: saved,
    follow = null,
    onchange,
  }: {
    /** `conversation`: the Primary's role setting; `new`: the next task this composer creates; `task`: the addressed task, changed at once. */
    kind: "conversation" | "new" | "task";
    /** `null` follows the role settings (only where `follow` is given). */
    value: RunAs | null;
    /** What following the settings means right now ("Claude Code"); `null`: there is nothing to follow. */
    follow?: string | null;
    /**
     * A returned promise settles once the change is saved and `value` shows it
     * (or once it failed); without one, `value` shows it at once.
     */
    onchange: (next: RunAs | null) => unknown;
  } = $props();

  /**
   * The last choice made here, held while it is being saved. Until then a
   * refetch carrying an older value — the echo of an earlier save — must not
   * replace it, and a second change builds on it.
   */
  let edited = $state<{ value: RunAs | null } | null>(null);
  let value = $derived(edited ? edited.value : saved);

  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings(), staleTime: 60_000 }));
  const agentsQuery = createQuery(() => ({ queryKey: ["agents"], queryFn: () => api.agents(), staleTime: 60_000 }));
  const catalogQuery = createQuery(() => ({
    queryKey: ["agent-models", value?.agent ?? ""],
    queryFn: () => api.agentModels(value?.agent ?? "claude"),
    enabled: value !== null,
    staleTime: 5 * 60_000,
  }));

  const ids = $props.id();
  /** A bottom sheet at phone width, a panel above the trigger elsewhere. */
  const phone = new MediaQuery("max-width: 639px");
  let open = $state(false);
  let triggerRef = $state<HTMLButtonElement | null>(null);
  /** What is typed in the model field and not committed yet: closing the panel still commits it. */
  let typed: string | null = null;

  const effortLabel = (effort: Effort): string => $t(`settings.effort.${effort || "default"}`);
  const providerLabel = (id: string): string => settingsQuery.data?.providers.find((p) => p.id === id)?.label ?? id;

  let available = $derived(new Map((agentsQuery.data?.agents ?? []).map((agent) => [agent.kind, agent.available])));
  let providers = $derived((settingsQuery.data?.providers ?? []).filter((provider) => value !== null && compatibility(value.agent, provider) === null));
  let providerOptions = $derived.by((): SelectOption[] => {
    const id = value?.provider ?? "";
    const options = [{ value: "", label: $t("settings.ownLogin") }, ...providers.map((provider) => ({ value: provider.id, label: provider.label }))];
    // A provider the choice names that no longer fits (or exists) is still listed, so the field shows the truth.
    return id && !providers.some((provider) => provider.id === id) ? [...options, { value: id, label: id }] : options;
  });
  let models = $derived.by((): ComboboxSuggestion[] => {
    if (value === null) return [];
    const provider = value.provider;
    if (provider !== "") return (settingsQuery.data?.providers.find((p) => p.id === provider)?.models ?? []).map((id) => ({ value: id }));
    return (catalogQuery.data?.models ?? []).map((model) => ({ value: model.id, label: model.label }));
  });
  let effortChoices = $derived<SelectOption[]>(
    value === null ? [] : effortOptions(value, value.provider === "" ? catalogQuery.data : undefined).map((effort) => ({ value: effort, label: effortLabel(effort) })),
  );
  let favorite = $derived(value !== null && favorites.list.some((entry) => sameRunAs(entry, value)));
  /** Why the current choice cannot run (a CLI gone, a provider deleted), if it cannot. */
  let problem = $derived(value === null ? null : unusable(value));

  let scopeName = $derived($t(kind === "conversation" ? "runAs.conversation" : "runAs.task"));
  let title = $derived($t(kind === "conversation" ? "runAs.conversationTitle" : kind === "new" ? "runAs.newTitle" : "runAs.taskTitle"));
  let scopeNote = $derived($t(kind === "conversation" ? "runAs.scopeConversation" : kind === "new" ? "runAs.scopeNew" : "runAs.scopeTask"));
  /** The trigger's text: the agent always, the rest only where there is room. */
  let head = $derived(value ? AGENT_LABELS[value.agent] : $t("runAs.followShort"));
  let tail = $derived.by(() => {
    if (!value) return follow ? ` · ${follow}` : "";
    return ` · ${value.model || $t("runAs.defaultModel")}${value.effort ? ` · ${effortLabel(value.effort)}` : ""}`;
  });
  let triggerLabel = $derived($t("runAs.trigger", { scope: scopeName, summary: head + tail }) + (problem ? ` (${problem})` : ""));

  /** Why a favorite cannot be applied here, if it cannot. */
  function unusable(entry: RunAs): string | null {
    if (available.get(entry.agent) === false) return $t("runAs.unavailable", { agent: AGENT_LABELS[entry.agent] });
    const issue = roleCompatibility(entry, settingsQuery.data?.providers ?? []);
    return issue ? $t(issue) : null;
  }

  function choose(next: RunAs | null): void {
    const edit = { value: next };
    edited = edit;
    typed = null;
    const result = onchange(next);
    const settled = () => {
      if (edited === edit) edited = null;
    };
    if (result instanceof Promise) void result.then(settled, settled);
    else settled();
  }

  function set(patch: Partial<RunAs>): void {
    if (value === null) return;
    choose({ ...value, ...patch });
  }

  function chooseAgent(agent: AgentKind | ""): void {
    if (agent === (value?.agent ?? "")) return;
    choose(withAgent(agent));
  }

  function commitModel(next: string): void {
    typed = null;
    if (value !== null && next !== value.model) set({ model: next });
  }

  function commitTyped(): void {
    if (typed !== null) commitModel(typed.trim());
  }

  function apply(entry: RunAs): void {
    open = false;
    if (!sameRunAs(entry, value)) choose({ ...entry });
  }

  function onOpenChange(isOpen: boolean): void {
    if (!isOpen) commitTyped();
  }

  /** Focus lands on the current choice, not on the first favorite. */
  function focusChoice(event: Event): void {
    event.preventDefault();
    document.getElementById(`${ids}-panel`)?.querySelector<HTMLElement>("[data-autofocus]")?.focus();
  }

  /** Back to the trigger by hand: WebKit never focused it on click, so there is nothing to restore. */
  function focusTrigger(event: Event): void {
    event.preventDefault();
    triggerRef?.focus();
  }

  const trigger =
    "flex h-6 max-w-full min-w-0 items-center gap-1.5 rounded-md px-1.5 text-xs text-foreground/85 transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70 data-[state=open]:bg-accent coarse:min-h-9";
  const pill = "h-6 rounded-md px-2 text-xs transition-colors disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-primary/70 coarse:min-h-9";
  const pillOff = "bg-accent text-foreground/85 hover:bg-accent-strong hover:text-foreground";
</script>

{#snippet face()}
  {#if kind === "conversation"}
    <MessageCircle size={13} class="shrink-0 text-muted" aria-hidden="true" />
  {:else}
    <Bot size={13} class="shrink-0 text-muted" aria-hidden="true" />
  {/if}
  <!-- Narrow: the icon alone names the scope, so both pickers fit on one row. -->
  <span class="hidden shrink-0 text-muted @sm:inline" aria-hidden="true">{scopeName}</span>
  <span class="min-w-0 truncate" aria-hidden="true">{head}<span class="hidden @lg:inline">{tail}</span></span>
  {#if problem}<TriangleAlert size={12} class="shrink-0 text-danger" aria-hidden="true" />{/if}
  <ChevronsUpDown size={12} class="shrink-0 text-muted" aria-hidden="true" />
{/snippet}

{#snippet panel()}
  <h2 id={`${ids}-title`} class="text-sm font-medium">{title}</h2>
  <p class="mt-0.5 text-xs text-muted">{scopeNote}</p>

  <section class="mt-3" aria-labelledby={`${ids}-favorites`}>
    <h3 id={`${ids}-favorites`} class="text-2xs font-medium text-muted">{$t("runAs.favorites")}</h3>
    {#if favorites.list.length === 0}
      <p class="mt-1 text-xs text-muted">{$t("runAs.favoritesEmpty")}</p>
    {:else}
      <ul class="mt-1 space-y-0.5">
        {#each favorites.list as entry (JSON.stringify(entry))}
          {@const label = favoriteLabel(entry, providerLabel, effortLabel, $t("runAs.defaultModel"))}
          {@const reason = unusable(entry)}
          {@const current = sameRunAs(entry, value)}
          <li class="flex items-center gap-1">
            <button
              type="button"
              class={cn(
                "flex min-w-0 flex-1 items-center gap-1.5 rounded-sm px-2 py-1 text-left text-xs hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary/70 disabled:opacity-60 disabled:hover:bg-transparent",
              )}
              aria-current={current ? "true" : undefined}
              disabled={reason !== null}
              onclick={() => apply(entry)}
            >
              <Check size={12} class={cn("shrink-0 text-primary", !current && "invisible")} aria-hidden="true" />
              <span class="min-w-0">
                <span class="block truncate">{label}</span>
                {#if reason}<span class="block text-2xs text-muted">{reason}</span>{/if}
              </span>
            </button>
            <button
              type="button"
              class="shrink-0 rounded-md p-1 text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70"
              aria-label={$t("runAs.removeFavorite", { label })}
              title={$t("runAs.removeFavorite", { label })}
              onclick={() => toggleFavoriteRun(entry)}
            >
              <X size={12} />
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <div class="mt-3 space-y-2.5 border-t border-border pt-3">
    <!-- One segmented control: a choice among siblings, not a row of separate buttons. -->
    <!-- The agents share equal widths; "follow settings" is a different kind of choice and takes what its label needs. -->
    <div class={cn(segmented, "grid w-full", follow !== null ? "grid-cols-[auto_repeat(3,minmax(0,1fr))]" : "grid-cols-3")} role="group" aria-label={$t("runAs.agent")}>
      {#if follow !== null}
        <button type="button" class={cn(segment, "px-1.5", value === null ? segmentOn : segmentOff)} aria-pressed={value === null} data-autofocus={value === null ? "" : undefined} onclick={() => chooseAgent("")}>
          {$t("runAs.followShort")}
        </button>
      {/if}
      {#each AGENT_KINDS as agent (agent)}
        {@const missing = available.get(agent) === false}
        <button
          type="button"
          class={cn(segment, "px-1.5", value?.agent === agent ? segmentOn : segmentOff)}
          aria-pressed={value?.agent === agent}
          disabled={missing && value?.agent !== agent}
          title={missing ? $t("runAs.unavailable", { agent: AGENT_LABELS[agent] }) : undefined}
          data-autofocus={value?.agent === agent ? "" : undefined}
          onclick={() => chooseAgent(agent)}
        >
          {AGENT_LABELS[agent]}
        </button>
      {/each}
    </div>

    {#if problem}
      <p class="flex items-start gap-1.5 text-xs text-danger" role="alert"><TriangleAlert size={14} class="mt-px shrink-0" />{problem}</p>
    {/if}
    {#if value === null}
      <p class="text-xs text-muted">{$t("runAs.follow", { summary: follow ?? "" })}</p>
    {:else}
      {#if providers.length > 0 || value.provider !== ""}
        <div class="space-y-1">
          <span class="block text-2xs text-muted" aria-hidden="true">{$t("runAs.provider")}</span>
          <Select size="sm" label={$t("runAs.provider")} value={value.provider} options={providerOptions} onchange={(next) => set({ provider: next, model: "" })} />
        </div>
      {/if}
      <div class="space-y-1">
        <span class="block text-2xs text-muted" aria-hidden="true">{$t("runAs.model")}</span>
        <Combobox
          size="sm"
          label={$t("runAs.model")}
          emptyLabel={$t("runAs.defaultModel")}
          value={value.model}
          suggestions={models}
          oninput={(text) => (typed = text)}
          onchange={commitModel}
        />
      </div>
      {#if value.provider === "" && catalogQuery.data?.error}
        <p class="text-xs text-muted">{$t("runAs.catalogFailed", { agent: AGENT_LABELS[value.agent] })}</p>
      {/if}
      <div class="space-y-1">
        <span class="block text-2xs text-muted" aria-hidden="true">{$t("runAs.effort")}</span>
        <Select size="sm" label={$t("runAs.effort")} value={value.effort} options={effortChoices} onchange={(next) => set({ effort: next as Effort })} />
      </div>
    {/if}
  </div>

  <div class="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
    <button
      type="button"
      class={cn(pill, "inline-flex items-center gap-1.5", favorite ? "bg-primary/10 text-foreground hover:bg-primary/15" : pillOff)}
      aria-pressed={favorite}
      title={favorite ? $t("runAs.favorited") : undefined}
      disabled={value === null}
      onclick={() => {
        commitTyped();
        if (value) toggleFavoriteRun(value);
      }}
    >
      <Star size={12} class={cn(favorite && "fill-current text-primary")} aria-hidden="true" />{$t("runAs.favorite")}
    </button>
    {#if kind === "conversation"}
      <button
        type="button"
        class={cn(pill, "inline-flex items-center gap-1.5 text-muted hover:bg-accent hover:text-foreground")}
        onclick={() => {
          open = false;
          openSettings("roles");
        }}
      >
        <Settings2 size={12} aria-hidden="true" />{$t("runAs.allRoles")}
      </button>
    {/if}
  </div>
{/snippet}

{#if phone.current}
  <Dialog.Root bind:open {onOpenChange}>
    <Dialog.Trigger bind:ref={triggerRef} class={trigger} aria-label={triggerLabel} title={problem ?? scopeNote}>{@render face()}</Dialog.Trigger>
    <Dialog.Portal>
      <Dialog.Overlay class="fixed inset-0 z-[60] animate-fade-in bg-overlay backdrop-blur-[2px]" />
      <Dialog.Content
        id={`${ids}-panel`}
        aria-labelledby={`${ids}-title`}
        class="fixed inset-x-0 bottom-0 z-[60] max-h-[80vh] overflow-y-auto animate-rise-in rounded-t-xl border border-border bg-popover p-4 pb-[max(1rem,env(safe-area-inset-bottom))] shadow-dialog outline-none"
        onOpenAutoFocus={focusChoice}
        onCloseAutoFocus={focusTrigger}
      >
        <div class={sheetHandle} aria-hidden="true"></div>
        {@render panel()}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
{:else}
  <Popover.Root bind:open {onOpenChange}>
    <Popover.Trigger bind:ref={triggerRef} class={trigger} aria-label={triggerLabel} title={problem ?? scopeNote}>{@render face()}</Popover.Trigger>
    <Popover.Portal>
      <Popover.Content
        id={`${ids}-panel`}
        side="top"
        align="start"
        sideOffset={6}
        collisionPadding={8}
        role="dialog"
        aria-labelledby={`${ids}-title`}
        class="z-[60] max-h-[var(--bits-popover-content-available-height)] w-96 animate-pop-in overflow-y-auto rounded-lg border border-border bg-popover p-3 shadow-popover outline-none"
        onOpenAutoFocus={focusChoice}
        onCloseAutoFocus={focusTrigger}
      >
        {@render panel()}
      </Popover.Content>
    </Popover.Portal>
  </Popover.Root>
{/if}
