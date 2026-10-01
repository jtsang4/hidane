<script lang="ts">
  import { FlaskConical, LoaderCircle, Save, TriangleAlert } from "@lucide/svelte";
  import { t } from "../../i18n/index.js";
  import type { AgentKind, AgentTestResult, Effort, ProviderView, Role, RoleConfig } from "../../lib/api.js";
  import { AGENT_KINDS, EFFORTS, errorText, roleCompatibility } from "../../lib/settings.js";
  import Button from "../ui/Button.svelte";
  import Card from "../ui/Card.svelte";
  import Input from "../ui/Input.svelte";
  import Select from "../ui/Select.svelte";

  let {
    role,
    config,
    providers,
    onsave,
    ontest,
  }: {
    role: Role;
    /** The saved configuration; the row edits a copy until Save. */
    config: RoleConfig;
    providers: readonly ProviderView[];
    onsave: (config: RoleConfig) => Promise<unknown>;
    ontest: () => Promise<AgentTestResult>;
  } = $props();

  // Re-seeded whenever the saved configuration changes, overwritten while editing.
  let draft = $derived<RoleConfig>({ ...config });
  let saving = $state(false);
  let testing = $state(false);
  let result = $state<AgentTestResult | null>(null);
  let testError = $state<string | null>(null);

  let issue = $derived(roleCompatibility(draft, providers));
  let dirty = $derived(
    draft.agent !== config.agent ||
      draft.provider !== config.provider ||
      draft.model !== config.model ||
      draft.effort !== config.effort,
  );
  let models = $derived(providers.find((provider) => provider.id === draft.provider)?.models ?? []);
  let listId = $derived(`role-models-${role}`);

  function update(patch: Partial<RoleConfig>): void {
    draft = { ...draft, ...patch };
  }

  async function save(): Promise<void> {
    saving = true;
    try {
      await onsave({ ...draft, model: draft.model.trim() });
    } catch {
      // The caller reports the failure; the draft stays so it can be corrected.
    } finally {
      saving = false;
    }
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

<Card class="space-y-3" role="group" aria-labelledby={`role-row-${role}`}>
  <div class="flex flex-wrap items-baseline gap-2">
    <h3 id={`role-row-${role}`} class="text-sm font-medium">{$t(`settings.roles.${role}`)}</h3>
    <span class="font-mono text-xs text-muted">{role}</span>
  </div>
  <div class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
    <label class="space-y-1 text-xs text-muted">
      <span>{$t("settings.roles.agent")}</span>
      <Select bind:value={() => draft.agent, (value) => update({ agent: value as AgentKind })}>
        {#each AGENT_KINDS as kind (kind)}
          <option value={kind}>{$t(`settings.kinds.${kind}`)}</option>
        {/each}
      </Select>
    </label>
    <label class="space-y-1 text-xs text-muted">
      <span>{$t("settings.roles.provider")}</span>
      <Select bind:value={() => draft.provider, (value) => update({ provider: value })}>
        <option value="">{$t("settings.ownLogin")}</option>
        {#each providers as provider (provider.id)}
          <option value={provider.id}>{provider.label} ({provider.id})</option>
        {/each}
        {#if draft.provider && !providers.some((provider) => provider.id === draft.provider)}
          <option value={draft.provider}>{draft.provider}</option>
        {/if}
      </Select>
    </label>
    <label class="space-y-1 text-xs text-muted">
      <span>{$t("settings.roles.model")}</span>
      <Input
        list={listId}
        class="font-mono"
        placeholder={$t("settings.roles.modelPlaceholder")}
        bind:value={() => draft.model, (value) => update({ model: value })}
      />
      <datalist id={listId}>
        {#each models as model (model)}
          <option value={model}></option>
        {/each}
      </datalist>
    </label>
    <label class="space-y-1 text-xs text-muted">
      <span>{$t("settings.roles.effort")}</span>
      <Select bind:value={() => draft.effort, (value) => update({ effort: value as Effort })}>
        {#each EFFORTS as effort (effort)}
          <option value={effort}>{$t(`settings.effort.${effort || "default"}`)}</option>
        {/each}
      </Select>
    </label>
  </div>
  {#if issue}
    <p class="flex items-start gap-1.5 text-xs text-danger" role="alert">
      <TriangleAlert size={14} class="mt-px shrink-0" />{$t(issue)}
    </p>
  {/if}
  <div class="flex flex-wrap items-center justify-end gap-2">
    {#if dirty}<span class="mr-auto text-xs text-muted">{$t("settings.roles.unsaved")}</span>{/if}
    <Button variant="outline" size="sm" disabled={testing || dirty} onclick={() => void test()}>
      {#if testing}<LoaderCircle size={14} class="animate-spin" />{:else}<FlaskConical size={14} />{/if}
      {$t("settings.roles.test")}
    </Button>
    <Button size="sm" disabled={saving || !dirty || issue !== null} onclick={() => void save()}>
      <Save size={14} />{$t("settings.roles.save")}
    </Button>
  </div>
  {#if testing}
    <p class="text-xs text-muted" role="status">{$t("settings.roles.testing")}</p>
  {:else if result}
    <div class="space-y-1 rounded-md bg-surface-2 p-2 text-xs" role="status">
      {#if result.ok}
        <p class="text-success">{$t("settings.roles.testOk", { agent: result.agent, model: result.model || $t("status.defaultModel"), ms: result.durationMs })}</p>
        {#if result.text}<pre class="max-h-40 overflow-auto whitespace-pre-wrap break-words font-mono text-foreground">{result.text}</pre>{/if}
      {:else}
        <p class="text-danger">{$t("settings.roles.testFailed", { ms: result.durationMs })}</p>
        {#if result.error}<pre class="max-h-40 overflow-auto whitespace-pre-wrap break-words font-mono text-danger">{result.error}</pre>{/if}
      {/if}
    </div>
  {:else if testError}
    <pre class="max-h-40 overflow-auto rounded-md bg-surface-2 p-2 text-xs whitespace-pre-wrap break-words text-danger" role="status">{testError}</pre>
  {/if}
</Card>
