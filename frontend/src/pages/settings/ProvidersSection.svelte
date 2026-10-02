<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Pencil, Plus, Trash2 } from "@lucide/svelte";
  import i18n, { t } from "../../i18n/index.js";
  import { api, type ProviderInput, type ProviderPatch, type ProviderView, type Role } from "../../lib/api.js";
  import { confirmAction } from "../../lib/confirm.svelte.js";
  import {
    draftFromPreset,
    draftFromProvider,
    emptyDraft,
    errorText,
    providerInput,
    providerPatch,
    rolesUsingProvider,
    type ProviderDraft,
  } from "../../lib/settings.js";
  import { pushToast } from "../../lib/toast.js";
  import ProviderForm from "../../components/settings/ProviderForm.svelte";
  import Badge from "../../components/ui/Badge.svelte";
  import Button from "../../components/ui/Button.svelte";
  import Select from "../../components/ui/Select.svelte";

  const queryClient = useQueryClient();
  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings() }));
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

  function providerSaved(message: string): void {
    editing = null;
    draft = emptyDraft();
    void queryClient.invalidateQueries({ queryKey: ["settings"] });
    void queryClient.invalidateQueries({ queryKey: ["status"] });
    pushToast(message, "default");
  }
  const createProvider = createMutation(() => ({
    mutationFn: (input: ProviderInput) => api.createProvider(input),
    onSuccess: () => providerSaved(i18n.t("settings.providers.created")),
    onError: reportError,
  }));
  const updateProvider = createMutation(() => ({
    mutationFn: (vars: { id: string; patch: ProviderPatch }) => api.updateProvider(vars.id, vars.patch),
    onSuccess: () => providerSaved(i18n.t("settings.providers.updated")),
    onError: reportError,
  }));
  const deleteProvider = createMutation(() => ({
    mutationFn: (id: string) => api.deleteProvider(id),
    onSuccess: () => {
      deleteError = null;
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      pushToast(i18n.t("settings.providers.deleted"), "default");
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

  async function remove(provider: ProviderView): Promise<void> {
    const confirmed = await confirmAction({
      title: i18n.t("settings.providers.confirmDelete", { label: provider.label }),
      body: i18n.t("settings.providers.confirmDeleteBody"),
      confirmLabel: i18n.t("common.delete"),
      destructive: true,
    });
    if (confirmed) deleteProvider.mutate(provider.id);
  }

  function roleNames(roles: Role[]): string {
    return roles.map((role) => $t(`settings.roles.${role}`)).join(", ");
  }

  let providerPending = $derived(createProvider.isPending || updateProvider.isPending);
</script>

<div class="flex flex-wrap items-start gap-3 px-1">
  <p class="min-w-0 flex-1 basis-64 text-xs text-muted">{$t("settings.providers.hint")}</p>
  {#if editing === null}
    <div class="flex flex-wrap items-center gap-2">
      {#if presets.length > 0}
        <!-- A menu of starting points, not a setting: it never keeps a value. -->
        <Select
          class="h-8 w-auto"
          size="sm"
          label={$t("settings.providers.presetPlaceholder")}
          placeholder={$t("settings.providers.presetPlaceholder")}
          value=""
          options={presets.map((preset) => ({ value: preset.id, label: preset.label }))}
          onchange={(id) => startCreate(id)}
        />
      {/if}
      <Button size="sm" onclick={() => startCreate()}><Plus size={16} />{$t("settings.providers.blank")}</Button>
    </div>
  {/if}
</div>

{#if editing?.mode === "create"}
  <ProviderForm bind:draft pending={providerPending} onsubmit={submitProvider} oncancel={cancelEdit} />
{/if}

{#if settings && providers.length === 0 && editing === null}
  <p class="py-10 text-center text-sm text-muted">{$t("settings.providers.empty")}</p>
{/if}

{#each providers as provider (provider.id)}
  {#if editing?.mode === "edit" && editing.id === provider.id}
    <ProviderForm bind:draft existing={provider} pending={providerPending} onsubmit={submitProvider} oncancel={cancelEdit} />
  {:else if settings}
    {@const usedBy = rolesUsingProvider(settings.roles, provider.id)}
    <div class="flex items-start gap-3 rounded-lg border border-border bg-surface p-4">
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
          <p class="font-mono text-xs break-all text-muted select-text">{provider.models.join(", ")}</p>
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
        <Button variant="ghost" size="icon" aria-label={`${$t("settings.providers.delete")} ${provider.label}`} disabled={deleteProvider.isPending} onclick={() => void remove(provider)}>
          <Trash2 size={16} class="text-danger" />
        </Button>
      </div>
    </div>
  {/if}
{/each}
