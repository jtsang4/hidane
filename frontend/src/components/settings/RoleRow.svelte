<script lang="ts">
  import { FlaskConical, LoaderCircle, TriangleAlert } from "@lucide/svelte";
  import { t } from "../../i18n/index.js";
  import type { AgentCatalog, AgentKind, AgentTestResult, Effort, ProviderView, Role, RoleConfig } from "../../lib/api.js";
  import { effortOptions } from "../../lib/runAs.js";
  import { AGENT_KINDS, effortsFor, errorText, roleCompatibility } from "../../lib/settings.js";
  import Button from "../ui/Button.svelte";
  import Combobox, { type ComboboxSuggestion } from "../ui/Combobox.svelte";
  import Select, { type SelectOption } from "../ui/Select.svelte";
  import SaveStatus from "./SaveStatus.svelte";
  import SettingsRow from "./SettingsRow.svelte";

  let {
    role,
    config,
    providers,
    catalogs = {},
    onsave,
    ontest,
  }: {
    role: Role;
    /** The saved configuration; the row edits a copy and saves each change as it is made. */
    config: RoleConfig;
    providers: readonly ProviderView[];
    /** What each CLI runs on its own login, for the model and effort lists while no provider is chosen. */
    catalogs?: Partial<Record<AgentKind, AgentCatalog>>;
    onsave: (config: RoleConfig) => Promise<unknown>;
    ontest: () => Promise<AgentTestResult>;
  } = $props();

  /**
   * What the person changed and is not saved yet. The row shows the saved
   * configuration under these, so a save that lands while another change is
   * being made cannot overwrite that change.
   */
  let edits = $state<Partial<RoleConfig>>({});
  let draft = $derived<RoleConfig>({ ...config, ...edits });
  let saving = $state(false);
  /** A change made while a save was in flight; saved once that one lands. */
  let again = false;
  let saveError = $state<string | null>(null);
  let savedOnce = $state(false);
  let testing = $state(false);
  let result = $state<AgentTestResult | null>(null);
  let testError = $state<string | null>(null);

  let issue = $derived(roleCompatibility(draft, providers));
  let dirty = $derived(
    draft.agent !== config.agent ||
      draft.provider !== config.provider ||
      draft.model.trim() !== config.model ||
      draft.effort !== config.effort,
  );
  let status = $derived<"idle" | "saving" | "saved" | "error" | "unsaved">(
    saving ? "saving" : saveError ? "error" : issue && dirty ? "unsaved" : savedOnce && !dirty ? "saved" : "idle",
  );
  let catalog = $derived(draft.provider === "" ? catalogs[draft.agent] : undefined);
  let models = $derived.by((): ComboboxSuggestion[] => {
    if (catalog) return catalog.models.map((model) => ({ value: model.id, label: model.label }));
    return (providers.find((provider) => provider.id === draft.provider)?.models ?? []).map((model) => ({ value: model }));
  });
  let ids = $derived({
    agent: `role-${role}-agent`,
    provider: `role-${role}-provider`,
    model: `role-${role}-model`,
    effort: `role-${role}-effort`,
  });
  let providerOptions = $derived<SelectOption[]>([
    { value: "", label: $t("settings.ownLogin") },
    ...providers.map((provider) => ({ value: provider.id, label: `${provider.label} (${provider.id})` })),
    // A provider removed since this role was saved is still what the role names.
    ...(draft.provider && !providers.some((provider) => provider.id === draft.provider) ? [{ value: draft.provider, label: draft.provider }] : []),
  ]);

  function update(patch: Partial<RoleConfig>): void {
    edits = { ...edits, ...patch };
    saveError = null;
  }

  /**
   * Saves what the row now shows. An agent × provider pair the CLI cannot
   * speak is kept on screen, unsaved, with the reason — the server would
   * refuse it anyway.
   */
  async function commit(): Promise<void> {
    if (saving) {
      again = true;
      return;
    }
    if (!dirty || issue !== null) return;
    const sent: RoleConfig = { ...draft, model: draft.model.trim() };
    saving = true;
    saveError = null;
    try {
      await onsave(sent);
      savedOnce = true;
      // Keep only what changed since this save was sent.
      const left: Partial<RoleConfig> = {};
      for (const key of Object.keys(edits) as (keyof RoleConfig)[]) {
        const value = edits[key];
        if (value !== undefined && (key === "model" ? value.trim() : value) !== sent[key]) Object.assign(left, { [key]: value });
      }
      edits = left;
    } catch (error) {
      saveError = errorText(error);
    } finally {
      saving = false;
      if (again) {
        again = false;
        void commit();
      }
    }
  }

  function change(patch: Partial<RoleConfig>): void {
    update(patch);
    void commit();
  }

  async function test(): Promise<void> {
    testing = true;
    result = null;
    testError = null;
    try {
      result = await ontest();
    } catch (error) {
      testError = errorText(error);
    } finally {
      testing = false;
    }
  }
</script>

{#snippet warning()}
  {#if issue}
    <p class="flex items-start gap-1.5 text-xs text-danger" role="alert">
      <TriangleAlert size={14} class="mt-px shrink-0" />{$t(issue)}
    </p>
  {/if}
{/snippet}

<div class="space-y-2" role="group" aria-labelledby={`role-row-${role}`}>
  <div class="flex items-end gap-3">
    <div class="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
      <h2 id={`role-row-${role}`} class="text-sm font-medium">{$t(`settings.roles.${role}`)}</h2>
      <span class="font-mono text-2xs text-muted">{role}</span>
    </div>
    <SaveStatus {status} error={saveError ?? ""} />
    <Button variant="secondary" disabled={testing || dirty || saving || issue !== null} title={dirty ? $t("settings.roles.testSaved") : undefined} onclick={() => void test()}>
      {#if testing}<LoaderCircle size={14} class="animate-spin" />{:else}<FlaskConical size={14} />{/if}
      {$t("settings.roles.test")}
    </Button>
  </div>
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    <SettingsRow wide label={$t("settings.roles.agent")} for={ids.agent}>
      <Select
        id={ids.agent}
        class="w-full sm:w-56"
        value={draft.agent}
        options={AGENT_KINDS.map((kind) => ({ value: kind, label: $t(`settings.kinds.${kind}`) }))}
        onchange={(next) => { const agent = next as AgentKind; change({ agent, ...(effortsFor(agent).includes(draft.effort) ? {} : { effort: "" }) }); }}
      />
    </SettingsRow>
    <SettingsRow wide label={$t("settings.roles.provider")} for={ids.provider} below={issue ? warning : undefined}>
      <Select id={ids.provider} class="w-full sm:w-56" value={draft.provider} options={providerOptions} onchange={(next) => change({ provider: next })} />
    </SettingsRow>
    <SettingsRow wide label={$t("settings.roles.model")} hint={$t("settings.roles.modelHint")} for={ids.model}>
      <Combobox
        id={ids.model}
        class="w-full sm:w-56"
        emptyLabel={$t("runAs.defaultModel")}
        value={draft.model}
        suggestions={models}
        oninput={(text) => update({ model: text })}
        onchange={(next) => change({ model: next })}
      />
    </SettingsRow>
    <SettingsRow wide label={$t("settings.roles.effort")} for={ids.effort}>
      <Select
        id={ids.effort}
        class="w-full sm:w-56"
        value={draft.effort}
        options={effortOptions(draft, catalog).map((effort) => ({ value: effort, label: $t(`settings.effort.${effort || "default"}`) }))}
        onchange={(next) => change({ effort: next as Effort })}
      />
    </SettingsRow>
    {#if testing || result || testError}
      <div class="px-3.5 py-2.5">
        {#if testing}
          <p class="text-xs text-muted" role="status">{$t("settings.roles.testing")}</p>
        {:else if result}
          <div class="space-y-1 text-xs" role="status">
            {#if result.ok}
              <p class="text-success">{$t("settings.roles.testOk", { agent: result.agent, model: result.model || $t("status.defaultModel"), ms: result.durationMs })}</p>
              {#if result.text}<pre class="max-h-40 overflow-auto rounded-md bg-background p-2 font-mono whitespace-pre-wrap break-words text-foreground select-text">{result.text}</pre>{/if}
            {:else}
              <p class="text-danger">{$t("settings.roles.testFailed", { ms: result.durationMs })}</p>
              {#if result.error}<pre class="max-h-40 overflow-auto rounded-md bg-background p-2 font-mono whitespace-pre-wrap break-words text-danger select-text">{result.error}</pre>{/if}
            {/if}
          </div>
        {:else if testError}
          <pre class="max-h-40 overflow-auto rounded-md bg-background p-2 text-xs whitespace-pre-wrap break-words text-danger select-text" role="status">{testError}</pre>
        {/if}
      </div>
    {/if}
  </div>
</div>
