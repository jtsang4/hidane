<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { OctagonAlert, Plus, Trash2 } from "@lucide/svelte";
  import i18n, { t } from "../../i18n/index.js";
  import { api, type PolicyRule } from "../../lib/api.js";
  import { confirmAction } from "../../lib/confirm.svelte.js";
  import { errorText } from "../../lib/settings.js";
  import { pushToast } from "../../lib/toast.js";
  import Badge from "../../components/ui/Badge.svelte";
  import Button from "../../components/ui/Button.svelte";
  import Input from "../../components/ui/Input.svelte";

  const queryClient = useQueryClient();
  let adding = $state(false);
  let pattern = $state("");
  let reason = $state("");
  let tools = $state("");

  const policyQuery = createQuery(() => ({ queryKey: ["policies"], queryFn: () => api.policies() }));
  let rules = $derived(policyQuery.data?.rules ?? []);
  /** POLICY.json exists but cannot be read: the guard then refuses every change. */
  let broken = $derived(policyQuery.data?.error ?? "");

  const add = createMutation(() => ({
    mutationFn: () =>
      api.addPolicy({
        pattern: pattern.trim(),
        reason: reason.trim(),
        tools: tools.split(",").map((tool) => tool.trim()).filter(Boolean),
      }),
    onSuccess: () => {
      pushToast(i18n.t("policies.added"), "default");
      adding = false;
      pattern = "";
      reason = "";
      tools = "";
      void queryClient.invalidateQueries({ queryKey: ["policies"] });
    },
    onError: (error) => pushToast(errorText(error)),
  }));
  const remove = createMutation(() => ({
    mutationFn: (id: string) => api.deletePolicy(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["policies"] }),
    onError: (error) => pushToast(errorText(error)),
  }));

  async function confirmRemove(rule: PolicyRule): Promise<void> {
    const confirmed = await confirmAction({
      title: i18n.t("policies.confirmDelete"),
      body: rule.pattern,
      confirmLabel: i18n.t("common.delete"),
      destructive: true,
    });
    if (confirmed) remove.mutate(rule.id);
  }
</script>

<div class="flex items-start gap-3 px-1">
  <div class="min-w-0 flex-1 space-y-1">
    <p class="text-xs text-muted">{$t("policies.subtitle")}</p>
    {#if policyQuery.data}<p class="font-mono text-[11px] break-all text-muted select-text">{$t("policies.file", { path: policyQuery.data.path })}</p>{/if}
  </div>
  {#if !adding}<Button size="sm" onclick={() => (adding = true)}><Plus size={16} />{$t("policies.add")}</Button>{/if}
</div>

{#if broken}
  <div class="flex items-start gap-2.5 rounded-lg border border-danger/50 bg-danger/10 p-4 text-sm" role="alert">
    <OctagonAlert size={18} class="mt-0.5 shrink-0 text-danger" aria-hidden="true" />
    <div class="min-w-0 space-y-1">
      <p class="font-medium text-danger">{$t("policies.broken")}</p>
      <p class="text-xs text-muted">{$t("policies.brokenHint")}</p>
      <pre class="mt-1 overflow-x-auto rounded bg-background p-2 font-mono text-xs whitespace-pre-wrap break-words text-danger select-text">{broken}</pre>
    </div>
  </div>
{/if}

{#if adding}
  <div class="space-y-2 rounded-lg border border-border bg-surface p-4">
    <Input bind:value={pattern} data-autofocus placeholder={$t("policies.pattern")} aria-label={$t("policies.pattern")} class="font-mono" />
    <Input bind:value={reason} placeholder={$t("policies.reason")} aria-label={$t("policies.reason")} />
    <Input bind:value={tools} placeholder={$t("policies.tools")} aria-label={$t("policies.tools")} />
    <div class="flex justify-end gap-2">
      <Button variant="outline" size="sm" onclick={() => (adding = false)}>{$t("common.cancel")}</Button>
      <Button size="sm" disabled={add.isPending || !pattern.trim() || !reason.trim()} onclick={() => add.mutate()}>{$t("policies.add")}</Button>
    </div>
  </div>
{/if}

{#if policyQuery.isLoading}<p class="px-1 text-sm text-muted">{$t("common.loading")}</p>{/if}
{#if !policyQuery.isLoading && rules.length === 0 && !broken}<p class="py-10 text-center text-sm text-muted">{$t("policies.empty")}</p>{/if}

{#if rules.length > 0}
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    {#each rules as rule (rule.id)}
      <div class="flex items-start gap-3 px-4 py-3">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <code class="rounded bg-surface-2 px-1.5 py-0.5 text-xs break-all select-text">{rule.pattern}</code>
            <Badge tone="muted">{rule.tools && rule.tools.length > 0 ? rule.tools.join(", ") : $t("policies.toolsAll")}</Badge>
            <span class="font-mono text-xs text-muted">{rule.id}</span>
          </div>
          <p class="mt-1.5 text-sm break-words select-text">{rule.reason}</p>
        </div>
        <Button variant="ghost" size="icon" aria-label={`${$t("policies.delete")} ${rule.id}`} disabled={remove.isPending} onclick={() => void confirmRemove(rule)}><Trash2 size={16} class="text-danger" /></Button>
      </div>
    {/each}
  </div>
{/if}
