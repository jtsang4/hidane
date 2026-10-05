<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import OctagonAlert from "@lucide/svelte/icons/octagon-alert";
  import Plus from "@lucide/svelte/icons/plus";
  import ShieldCheck from "@lucide/svelte/icons/shield-check";
  import Trash2 from "@lucide/svelte/icons/trash";
  import i18n, { t } from "../../i18n/index.js";
  import { api, type PolicyRule } from "../../lib/api.js";
  import { confirmAction } from "../../lib/confirm.svelte.js";
  import { pushToast, toastError } from "../../lib/toast.js";
  import Badge from "../../components/ui/Badge.svelte";
  import PathText from "../../components/PathText.svelte";
  import EmptyState from "../../components/EmptyState.svelte";
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
    onError: (error) => toastError(error),
  }));
  const remove = createMutation(() => ({
    mutationFn: (id: string) => api.deletePolicy(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["policies"] }),
    onError: (error) => toastError(error),
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

<div class="flex items-start gap-3">
  <div class="min-w-0 flex-1 space-y-1">
    <p class="text-xs text-muted">{$t("policies.subtitle")}</p>
    {#if policyQuery.data}{@const path = policyQuery.data.path}<p class="font-mono text-2xs break-words text-muted select-text">{#each $t("policies.file", { path: "\u0000" }).split("\u0000") as piece, index (index)}{#if index > 0}<PathText {path} />{/if}{piece}{/each}</p>{/if}
  </div>
  {#if !adding}<Button variant="soft" onclick={() => (adding = true)}><Plus size={16} />{$t("policies.add")}</Button>{/if}
</div>

{#if broken}
  <div class="flex items-start gap-2.5 rounded-lg border border-danger/40 bg-danger/8 p-3 text-sm" role="alert">
    <OctagonAlert size={18} class="mt-0.5 shrink-0 text-danger" aria-hidden="true" />
    <div class="min-w-0 space-y-1">
      <p class="font-medium text-danger">{$t("policies.broken")}</p>
      <p class="text-xs text-muted">{$t("policies.brokenHint")}</p>
      <pre class="mt-1 overflow-x-auto rounded-sm bg-background p-2 font-mono text-xs whitespace-pre-wrap break-words text-danger select-text">{broken}</pre>
    </div>
  </div>
{/if}

{#if adding}
  <div class="space-y-2 rounded-lg border border-border bg-surface p-3">
    <Input bind:value={pattern} data-autofocus placeholder={$t("policies.pattern")} aria-label={$t("policies.pattern")} class="font-mono" />
    <Input bind:value={reason} placeholder={$t("policies.reason")} aria-label={$t("policies.reason")} />
    <Input bind:value={tools} placeholder={$t("policies.tools")} aria-label={$t("policies.tools")} />
    <div class="flex justify-end gap-2">
      <Button variant="secondary" onclick={() => (adding = false)}>{$t("common.cancel")}</Button>
      <Button disabled={add.isPending || !pattern.trim() || !reason.trim()} onclick={() => add.mutate()}>{$t("policies.add")}</Button>
    </div>
  </div>
{/if}

{#if policyQuery.isLoading}<p class="text-sm text-muted">{$t("common.loading")}</p>{/if}
{#if !policyQuery.isLoading && rules.length === 0 && !broken}<EmptyState icon={ShieldCheck} text={$t("policies.empty")} class="py-12" />{/if}

{#if rules.length > 0}
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    {#each rules as rule (rule.id)}
      <div class="flex items-start gap-3 px-3.5 py-2.5">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <code class="rounded-sm bg-accent px-1.5 py-px text-xs break-all select-text">{rule.pattern}</code>
            <Badge tone="muted">{rule.tools && rule.tools.length > 0 ? rule.tools.join(", ") : $t("policies.toolsAll")}</Badge>
            <span class="font-mono text-xs text-muted">{rule.id}</span>
          </div>
          <p class="mt-1.5 text-sm break-words select-text">{rule.reason}</p>
        </div>
        <Button variant="ghost" size="icon" class="hover:bg-danger/10 hover:text-danger" aria-label={`${$t("policies.delete")} ${rule.id}`} disabled={remove.isPending} onclick={() => void confirmRemove(rule)}><Trash2 /></Button>
      </div>
    {/each}
  </div>
{/if}
