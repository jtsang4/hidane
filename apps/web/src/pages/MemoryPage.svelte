<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Plus, Trash2 } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, ApiError, type MemoryEntry } from "../lib/api.js";
  import { pushToast } from "../lib/toast.js";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Card from "../components/ui/Card.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  const queryClient = useQueryClient();
  let draft = $state<string | null>(null);
  let kind = $state<MemoryEntry["kind"]>("preference");
  let draftValue = $derived(draft ?? "");
  const memoryKinds = ["fact", "preference", "decision", "lesson"] as const;
  const kindKey = (value: MemoryEntry["kind"]) => `memory.kind.${value}` as "memory.kind.fact" | "memory.kind.preference" | "memory.kind.decision" | "memory.kind.lesson";
  const memoryQuery = createQuery(() => ({ queryKey: ["memories"], queryFn: () => api.memories() }));
  let entries = $derived(memoryQuery.data?.entries ?? []);

  const forget = createMutation(() => ({
    mutationFn: (id: string) => api.forgetMemory(id),
    onSuccess: () => {
      pushToast($t("memory.forgotten"), "default");
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));
  const add = createMutation(() => ({
    mutationFn: (content: string) => api.addMemory(kind, content),
    onSuccess: () => {
      pushToast($t("memory.added"), "default");
      draft = null;
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));

  const tones: Record<MemoryEntry["kind"], "default" | "success" | "danger" | "muted"> = {
    fact: "muted",
    preference: "default",
    decision: "success",
    lesson: "danger",
  };
</script>

<div class="space-y-3 p-4">
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0">
      <h1 class="text-lg font-semibold">{$t("memory.title")}</h1>
      <p class="mt-1 text-xs break-all text-muted">{$t("memory.subtitle", { path: memoryQuery.data?.path ?? "" })}</p>
    </div>
    {#if draft === null}<Button size="sm" onclick={() => (draft = "")}><Plus size={16} />{$t("memory.add")}</Button>{/if}
  </div>
  {#if draft !== null}
    <Card class="space-y-2">
      <div class="flex flex-wrap gap-2">
        {#each memoryKinds as option (option)}
          <Button size="sm" variant={kind === option ? "default" : "outline"} onclick={() => (kind = option)}>{$t(kindKey(option))}</Button>
        {/each}
      </div>
      <Textarea rows={2} autofocus bind:value={draft} placeholder={$t("memory.addPlaceholder")} />
      <div class="flex justify-end gap-2">
        <Button variant="outline" size="sm" onclick={() => (draft = null)}>{$t("common.cancel")}</Button>
        <Button size="sm" disabled={add.isPending || draftValue.trim().length === 0} onclick={() => add.mutate(draftValue.trim())}>{$t("memory.save")}</Button>
      </div>
    </Card>
  {/if}
  {#if memoryQuery.isLoading}<p class="text-sm text-muted">{$t("common.loading")}</p>{/if}
  {#if !memoryQuery.isLoading && entries.length === 0}<p class="pt-8 text-center text-sm text-muted">{$t("memory.empty")}</p>{/if}
  {#each entries as entry (entry.id)}
    <Card class="flex items-start gap-3">
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-2">
          <Badge tone={tones[entry.kind]}>{$t(`memory.kind.${entry.kind}` as const)}</Badge>
          <span class="text-xs text-muted">{entry.date}</span>
          <span class="font-mono text-xs text-muted">{entry.id}</span>
        </div>
        <p class="mt-2 text-sm break-words whitespace-pre-wrap">{entry.content}</p>
      </div>
      <Button variant="ghost" size="icon" aria-label={`${$t("memory.forget")} ${entry.id}`} disabled={forget.isPending} onclick={() => { if (confirm($t("memory.confirm"))) forget.mutate(entry.id); }}><Trash2 size={16} class="text-danger" /></Button>
    </Card>
  {/each}
</div>
