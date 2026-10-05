<script lang="ts">
  import ExternalLink from "@lucide/svelte/icons/external-link";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import { t } from "../../i18n/index.js";
  import type { ProviderView } from "../../lib/api.js";
  import { draftProblem, type ProviderDraft } from "../../lib/settings.js";
  import Button from "../ui/Button.svelte";
  import Card from "../ui/Card.svelte";
  import Input from "../ui/Input.svelte";
  import Textarea from "../ui/Textarea.svelte";

  let {
    draft = $bindable(),
    existing,
    pending,
    onsubmit,
    oncancel,
  }: {
    draft: ProviderDraft;
    /** Set when editing: the stored provider, for its id and key hint. */
    existing?: ProviderView | undefined;
    pending: boolean;
    onsubmit: () => void;
    oncancel: () => void;
  } = $props();

  let problem = $derived(draftProblem(draft));
  let fieldId = $derived(existing ? `provider-${existing.id}` : "provider-new");
</script>

<Card class="space-y-3">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h3 class="text-sm font-medium">
      {existing ? $t("settings.providers.editTitle", { label: existing.label }) : $t("settings.providers.newTitle")}
    </h3>
    {#if draft.docsUrl}
      <a class="flex items-center gap-1 text-xs text-primary underline" href={draft.docsUrl} target="_blank" rel="noreferrer noopener">
        {$t("settings.providers.docs")}<ExternalLink size={12} />
      </a>
    {/if}
  </div>
  <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
    <label class="space-y-1 text-xs text-muted" for={`${fieldId}-label`}>
      <span>{$t("settings.providers.label")}</span>
      <Input id={`${fieldId}-label`} bind:value={draft.label} />
    </label>
    {#if existing}
      <div class="space-y-1 text-xs text-muted">
        <span>{$t("settings.providers.idLabel")}</span>
        <p class="flex h-7 items-center font-mono text-sm text-foreground">{existing.id}</p>
      </div>
    {:else}
      <label class="space-y-1 text-xs text-muted" for={`${fieldId}-id`}>
        <span>{$t("settings.providers.id")}</span>
        <Input id={`${fieldId}-id`} class="font-mono" bind:value={draft.id} />
      </label>
    {/if}
    <label class="space-y-1 text-xs text-muted sm:col-span-2" for={`${fieldId}-anthropic`}>
      <span>{$t("settings.providers.anthropicBaseUrl")}</span>
      <Input id={`${fieldId}-anthropic`} class="font-mono" type="url" placeholder="https://" bind:value={draft.anthropicBaseUrl} />
    </label>
    <label class="space-y-1 text-xs text-muted sm:col-span-2" for={`${fieldId}-openai`}>
      <span>{$t("settings.providers.openaiBaseUrl")}</span>
      <Input id={`${fieldId}-openai`} class="font-mono" type="url" placeholder="https://" bind:value={draft.openaiBaseUrl} />
    </label>
    <label class="space-y-1 text-xs text-muted" for={`${fieldId}-pi`}>
      <span>{$t("settings.providers.piProvider")}</span>
      <Input id={`${fieldId}-pi`} class="font-mono" bind:value={draft.piProvider} />
    </label>
    <div class="space-y-1 text-xs text-muted">
      <label for={`${fieldId}-key`}>{$t("settings.providers.apiKey")}</label>
      {#if draft.clearKey}
        <div class="flex h-7 items-center gap-2">
          <span class="text-danger">{$t("settings.providers.keyWillClear")}</span>
          <Button variant="ghost" size="sm" onclick={() => (draft.clearKey = false)}>{$t("settings.providers.undoClear")}</Button>
        </div>
      {:else}
        <div class="flex gap-2">
          <Input
            id={`${fieldId}-key`}
            type="password"
            autocomplete="off"
            class="font-mono"
            placeholder={existing?.hasApiKey ? $t("settings.providers.apiKeyKeep", { hint: existing.apiKeyHint }) : ""}
            bind:value={draft.apiKey}
          />
          {#if existing?.hasApiKey}
            <Button variant="secondary" onclick={() => { draft.apiKey = ""; draft.clearKey = true; }}>
              <KeyRound size={14} />{$t("settings.providers.clearKey")}
            </Button>
          {/if}
        </div>
      {/if}
    </div>
    <label class="space-y-1 text-xs text-muted sm:col-span-2" for={`${fieldId}-models`}>
      <span>{$t("settings.providers.models")}</span>
      <Textarea id={`${fieldId}-models`} rows={3} class="font-mono" bind:value={draft.models} />
    </label>
  </div>
  {#if problem}<p class="text-xs text-muted">{$t(problem)}</p>{/if}
  <div class="flex justify-end gap-2">
    <Button variant="secondary" onclick={oncancel}>{$t("common.cancel")}</Button>
    <Button disabled={pending || problem !== null} onclick={onsubmit}>
      {existing ? $t("settings.providers.save") : $t("settings.providers.create")}
    </Button>
  </div>
</Card>
