<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Pencil, Plus, RefreshCw, SlidersHorizontal, Trash2 } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import {
    api,
    type AgentKind,
    type ProviderPatch,
    type ProviderInput,
    type ProviderView,
    type Role,
    type RoleConfig,
    type Settings,
  } from "../lib/api.js";
  import {
    AGENT_KINDS,
    ROLES,
    draftFromPreset,
    draftFromProvider,
    emptyDraft,
    errorText,
    providerInput,
    providerPatch,
    rolesUsingProvider,
    type ProviderDraft,
  } from "../lib/settings.js";
  import { pushToast } from "../lib/toast.js";
  import AgentRow from "../components/settings/AgentRow.svelte";
  import ProviderForm from "../components/settings/ProviderForm.svelte";
  import RoleRow from "../components/settings/RoleRow.svelte";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Card from "../components/ui/Card.svelte";
  import Select from "../components/ui/Select.svelte";

  const queryClient = useQueryClient();
  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings() }));
  // Detection runs `<cli> --version` for each CLI; it does not need the default 3s staleness.
  const agentsQuery = createQuery(() => ({ queryKey: ["agents"], queryFn: () => api.agents(), staleTime: 60_000 }));
  const presetsQuery = createQuery(() => ({
    queryKey: ["provider-presets"],
    queryFn: () => api.providerPresets(),
    staleTime: Infinity,
  }));

  let settings = $derived(settingsQuery.data);
  let providers = $derived(settings?.providers ?? []);
  let presets = $derived(presetsQuery.data?.presets ?? []);

  type Editing = { mode: "create" } | { mode: "edit"; id: string };
  let editing = $state<Editing | null>(null);
  let draft = $state<ProviderDraft>(emptyDraft());
  let deleteError = $state<{ id: string; message: string } | null>(null);

  const reportError = (error: unknown) => pushToast(errorText(error));

  function applied(next: Settings): void {
    queryClient.setQueryData(["settings"], next);
    void queryClient.invalidateQueries({ queryKey: ["settings"] });
    void queryClient.invalidateQueries({ queryKey: ["status"] });
  }

  const saveRoles = createMutation(() => ({
    mutationFn: (roles: Partial<Record<Role, RoleConfig>>) => api.saveRoles(roles),
    onSuccess: (response: { settings: Settings }) => {
      applied(response.settings);
      pushToast($t("settings.roles.saved"), "default");
    },
    onError: reportError,
  }));
  const saveBinaries = createMutation(() => ({
    mutationFn: (binaries: Partial<Record<AgentKind, string>>) => api.saveBinaries(binaries),
    onSuccess: (response: { settings: Settings }) => {
      applied(response.settings);
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
      pushToast($t("settings.agents.saved"), "default");
    },
    onError: reportError,
  }));
  // A failed test is shown on its row, not as a toast.
  const testRole = createMutation(() => ({ mutationFn: (role: Role) => api.testAgent(role) }));

  function providerSaved(message: string): void {
    editing = null;
    draft = emptyDraft();
    void queryClient.invalidateQueries({ queryKey: ["settings"] });
    void queryClient.invalidateQueries({ queryKey: ["status"] });
    pushToast(message, "default");
  }
  const createProvider = createMutation(() => ({
    mutationFn: (input: ProviderInput) => api.createProvider(input),
    onSuccess: () => providerSaved($t("settings.providers.created")),
    onError: reportError,
  }));
  const updateProvider = createMutation(() => ({
    mutationFn: (vars: { id: string; patch: ProviderPatch }) => api.updateProvider(vars.id, vars.patch),
    onSuccess: () => providerSaved($t("settings.providers.updated")),
    onError: reportError,
  }));
  const deleteProvider = createMutation(() => ({
    mutationFn: (id: string) => api.deleteProvider(id),
    onSuccess: () => {
      deleteError = null;
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      pushToast($t("settings.providers.deleted"), "default");
    },
    // 409 when a role still uses the provider: the server's reason is the useful part.
    onError: (error: unknown, id: string) => {
      const message = errorText(error);
      deleteError = { id, message };
      pushToast(message);
    },
  }));

  function startCreate(presetId?: string): void {
    const preset = presets.find((candidate) => candidate.id === presetId);
    draft = preset ? draftFromPreset(preset, providers.map((provider) => provider.id)) : emptyDraft();
    editing = { mode: "create" };
  }

  function startEdit(provider: ProviderView): void {
    draft = draftFromProvider(provider);
    editing = { mode: "edit", id: provider.id };
  }

  function cancelEdit(): void {
    editing = null;
    draft = emptyDraft();
  }

  function submitProvider(): void {
    if (!editing) return;
    if (editing.mode === "create") createProvider.mutate(providerInput(draft));
    else updateProvider.mutate({ id: editing.id, patch: providerPatch(draft) });
  }

  function remove(provider: ProviderView): void {
    if (confirm($t("settings.providers.confirmDelete", { label: provider.label }))) deleteProvider.mutate(provider.id);
  }

  function onPreset(event: Event & { currentTarget: HTMLSelectElement }): void {
    const id = event.currentTarget.value;
    event.currentTarget.value = "";
    if (id) startCreate(id);
  }

  function roleNames(roles: Role[]): string {
    return roles.map((role) => $t(`settings.roles.${role}`)).join(", ");
  }

  let providerPending = $derived(createProvider.isPending || updateProvider.isPending);
</script>

<div class="space-y-6 p-4">
  <div class="min-w-0">
    <h1 class="flex items-center gap-2 text-lg font-semibold"><SlidersHorizontal size={18} class="text-primary" />{$t("settings.title")}</h1>
    <p class="mt-1 text-xs text-muted">{$t("settings.subtitle")}</p>
    {#if settings}<p class="mt-1 font-mono text-[11px] break-all text-muted">{$t("settings.file", { path: settings.path })}</p>{/if}
  </div>

  {#if settingsQuery.isLoading}
    <p class="text-sm text-muted">{$t("common.loading")}</p>
  {:else if settings}
    <section class="space-y-3" aria-labelledby="settings-agents">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 id="settings-agents" class="text-base font-semibold">{$t("settings.agents.title")}</h2>
          <p class="mt-1 text-xs text-muted">{$t("settings.agents.hint")}</p>
        </div>
        <Button variant="outline" size="sm" disabled={agentsQuery.isFetching} onclick={() => void agentsQuery.refetch()}>
          <RefreshCw size={14} class={agentsQuery.isFetching ? "animate-spin" : ""} />{$t("settings.agents.redetect")}
        </Button>
      </div>
      <div class="grid grid-cols-1 gap-3 lg:grid-cols-3">
        {#each AGENT_KINDS as kind (kind)}
          <AgentRow
            {kind}
            info={agentsQuery.data?.agents.find((agent) => agent.kind === kind)}
            detecting={agentsQuery.isFetching}
            savedPath={settings.binaries[kind] ?? ""}
            onsave={(path) => saveBinaries.mutateAsync({ ...settings.binaries, [kind]: path })}
          />
        {/each}
      </div>
    </section>

    <section class="space-y-3" aria-labelledby="settings-roles">
      <div>
        <h2 id="settings-roles" class="text-base font-semibold">{$t("settings.roles.title")}</h2>
        <p class="mt-1 text-xs text-muted">{$t("settings.roles.hint")}</p>
      </div>
      {#each ROLES as role (role)}
        <RoleRow
          {role}
          config={settings.roles[role]}
          {providers}
          onsave={(config) => saveRoles.mutateAsync({ ...settings.roles, [role]: config })}
          ontest={() => testRole.mutateAsync(role)}
        />
      {/each}
    </section>

    <section class="space-y-3" aria-labelledby="settings-providers">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 id="settings-providers" class="text-base font-semibold">{$t("settings.providers.title")}</h2>
          <p class="mt-1 text-xs text-muted">{$t("settings.providers.hint")}</p>
        </div>
        {#if editing === null}
          <div class="flex flex-wrap items-center gap-2">
            {#if presets.length > 0}
              <Select class="h-8 w-auto text-xs" aria-label={$t("settings.providers.presetPlaceholder")} onchange={onPreset}>
                <option value="">{$t("settings.providers.presetPlaceholder")}</option>
                {#each presets as preset (preset.id)}
                  <option value={preset.id}>{preset.label}</option>
                {/each}
              </Select>
            {/if}
            <Button size="sm" onclick={() => startCreate()}><Plus size={16} />{$t("settings.providers.blank")}</Button>
          </div>
        {/if}
      </div>

      {#if editing?.mode === "create"}
        <ProviderForm bind:draft pending={providerPending} onsubmit={submitProvider} oncancel={cancelEdit} />
      {/if}

      {#if providers.length === 0 && editing === null}
        <p class="pt-4 text-center text-sm text-muted">{$t("settings.providers.empty")}</p>
      {/if}

      {#each providers as provider (provider.id)}
        {#if editing?.mode === "edit" && editing.id === provider.id}
          <ProviderForm bind:draft existing={provider} pending={providerPending} onsubmit={submitProvider} oncancel={cancelEdit} />
        {:else}
          {@const usedBy = rolesUsingProvider(settings.roles, provider.id)}
          <Card class="flex items-start gap-3">
            <div class="min-w-0 flex-1 space-y-2">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-sm font-medium">{provider.label}</span>
                <span class="font-mono text-xs text-muted">{provider.id}</span>
                {#if provider.anthropicBaseUrl}<Badge tone="muted">{$t("settings.providers.badgeAnthropic")}</Badge>{/if}
                {#if provider.openaiBaseUrl}<Badge tone="muted">{$t("settings.providers.badgeOpenai")}</Badge>{/if}
                {#if provider.piProvider}<Badge tone="muted">{$t("settings.providers.badgePi", { name: provider.piProvider })}</Badge>{/if}
                {#if provider.hasApiKey}
                  <Badge tone="success">{$t("settings.providers.keyHint", { hint: provider.apiKeyHint })}</Badge>
                {:else}
                  <Badge tone="danger">{$t("settings.providers.noKey")}</Badge>
                {/if}
              </div>
              {#if provider.models.length > 0}
                <p class="font-mono text-xs break-all text-muted">{provider.models.join(", ")}</p>
              {/if}
              {#if usedBy.length > 0}
                <p class="text-xs text-muted">{$t("settings.providers.usedBy", { roles: roleNames(usedBy) })}</p>
              {/if}
              {#if deleteError?.id === provider.id}
                <p class="text-xs break-words text-danger" role="alert">{deleteError.message}</p>
              {/if}
            </div>
            <div class="flex shrink-0 gap-1">
              <Button variant="ghost" size="icon" aria-label={`${$t("settings.providers.edit")} ${provider.label}`} disabled={editing !== null} onclick={() => startEdit(provider)}>
                <Pencil size={16} />
              </Button>
              <Button variant="ghost" size="icon" aria-label={`${$t("settings.providers.delete")} ${provider.label}`} disabled={deleteProvider.isPending} onclick={() => remove(provider)}>
                <Trash2 size={16} class="text-danger" />
              </Button>
            </div>
          </Card>
        {/if}
      {/each}
    </section>
  {/if}
</div>
