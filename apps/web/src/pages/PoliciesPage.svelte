<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Plus, ShieldCheck, Trash2 } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, ApiError } from "../lib/api.js";
  import { pushToast } from "../lib/toast.js";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Card from "../components/ui/Card.svelte";
  import Input from "../components/ui/Input.svelte";

  const queryClient = useQueryClient();
  let adding = $state(false);
  let pattern = $state("");
  let reason = $state("");
  let tools = $state("");

  const policyQuery = createQuery(() => ({ queryKey: ["policies"], queryFn: () => api.policies() }));
  let rules = $derived(policyQuery.data?.rules ?? []);

  const add = createMutation(() => ({
    mutationFn: () =>
      api.addPolicy({
        pattern: pattern.trim(),
        reason: reason.trim(),
        tools: tools.split(",").map((tool) => tool.trim()).filter(Boolean),
      }),
    onSuccess: () => {
      pushToast($t("policies.added"), "default");
      adding = false;
      pattern = "";
      reason = "";
      tools = "";
      void queryClient.invalidateQueries({ queryKey: ["policies"] });
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));
  const remove = createMutation(() => ({
    mutationFn: (id: string) => api.deletePolicy(id),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["policies"] }),
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));
</script>

<div class="space-y-3 p-4">
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0">
      <h1 class="flex items-center gap-2 text-lg font-semibold"><ShieldCheck size={18} class="text-primary" />{$t("policies.title")}</h1>
      <p class="mt-1 text-xs text-muted">{$t("policies.subtitle")}</p>
      {#if policyQuery.data}<p class="mt-1 font-mono text-[11px] break-all text-muted">{$t("policies.file", { path: policyQuery.data.path })}</p>{/if}
    </div>
    {#if !adding}<Button size="sm" onclick={() => (adding = true)}><Plus size={16} />{$t("policies.add")}</Button>{/if}
  </div>
  {#if adding}
    <Card class="space-y-2">
      <Input bind:value={pattern} autofocus placeholder={$t("policies.pattern")} aria-label={$t("policies.pattern")} class="font-mono" />
      <Input bind:value={reason} placeholder={$t("policies.reason")} aria-label={$t("policies.reason")} />
      <Input bind:value={tools} placeholder={$t("policies.tools")} aria-label={$t("policies.tools")} />
      <div class="flex justify-end gap-2">
        <Button variant="outline" size="sm" onclick={() => (adding = false)}>{$t("common.cancel")}</Button>
        <Button size="sm" disabled={add.isPending || !pattern.trim() || !reason.trim()} onclick={() => add.mutate()}>{$t("policies.add")}</Button>
      </div>
    </Card>
  {/if}
  {#if policyQuery.isLoading}<p class="text-sm text-muted">{$t("common.loading")}</p>{/if}
  {#if !policyQuery.isLoading && rules.length === 0}<p class="pt-8 text-center text-sm text-muted">{$t("policies.empty")}</p>{/if}
  {#each rules as rule (rule.id)}
    <Card class="flex items-start gap-3">
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <code class="rounded bg-surface-2 px-1.5 py-0.5 text-xs break-all">{rule.pattern}</code>
          <Badge tone="muted">{rule.tools && rule.tools.length > 0 ? rule.tools.join(", ") : $t("policies.toolsAll")}</Badge>
          <span class="font-mono text-xs text-muted">{rule.id}</span>
        </div>
        <p class="mt-2 text-sm break-words">{rule.reason}</p>
      </div>
      <Button variant="ghost" size="icon" aria-label={`${$t("policies.delete")} ${rule.id}`} disabled={remove.isPending} onclick={() => { if (confirm($t("policies.confirmDelete"))) remove.mutate(rule.id); }}><Trash2 size={16} class="text-danger" /></Button>
    </Card>
  {/each}
</div>
