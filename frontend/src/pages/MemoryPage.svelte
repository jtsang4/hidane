<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Plus, Trash2 } from "@lucide/svelte";
  import i18n, { t } from "../i18n/index.js";
  import { api, type MemoryEntry } from "../lib/api.js";
  import { confirmAction } from "../lib/confirm.svelte.js";
  import { errorText } from "../lib/settings.js";
  import { pushToast } from "../lib/toast.js";
  import Page from "../components/Page.svelte";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  const queryClient = useQueryClient();
  let draft = $state<string | null>(null);
  let kind = $state<MemoryEntry["kind"]>("preference");
  let draftValue = $derived(draft ?? "");
  const memoryKinds = ["fact", "preference", "decision", "lesson"] as const;
  const memoryQuery = createQuery(() => ({ queryKey: ["memories"], queryFn: () => api.memories() }));
  let entries = $derived(memoryQuery.data?.entries ?? []);
  let workItems = $derived(memoryQuery.data?.workItems ?? []);

  const forget = createMutation(() => ({
    mutationFn: (id: string) => api.forgetMemory(id),
    onSuccess: () => {
      pushToast(i18n.t("memory.forgotten"), "default");
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (error) => pushToast(errorText(error)),
  }));
  const add = createMutation(() => ({
    mutationFn: (content: string) => api.addMemory(kind, content),
    onSuccess: () => {
      pushToast(i18n.t("memory.added"), "default");
      draft = null;
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (error) => pushToast(errorText(error)),
  }));

  async function confirmForget(entry: MemoryEntry): Promise<void> {
    const confirmed = await confirmAction({
      title: i18n.t("memory.confirmTitle"),
      body: `${entry.content}\n\n${i18n.t("memory.confirm")}`,
      confirmLabel: i18n.t("memory.forget"),
      destructive: true,
    });
    if (confirmed) forget.mutate(entry.id);
  }

  const tones: Record<MemoryEntry["kind"], "default" | "success" | "danger" | "muted"> = {
    fact: "muted",
    preference: "default",
    decision: "success",
    lesson: "danger",
  };
</script>

{#snippet entryList(list: MemoryEntry[])}
  <ul class="divide-y divide-border rounded-lg border border-border bg-surface">
    {#each list as entry (entry.id)}
      <li class="flex items-start gap-3 px-4 py-3">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <Badge tone={tones[entry.kind]}>{$t(`memory.kind.${entry.kind}`)}</Badge>
            <span class="text-xs text-muted">{entry.date}</span>
            <span class="font-mono text-xs text-muted">{entry.id}</span>
          </div>
          <p class="mt-1.5 text-sm break-words whitespace-pre-wrap select-text">{entry.content}</p>
        </div>
        <Button variant="ghost" size="icon" aria-label={`${$t("memory.forget")} ${entry.id}`} disabled={forget.isPending} onclick={() => void confirmForget(entry)}><Trash2 size={16} class="text-danger" /></Button>
      </li>
    {/each}
  </ul>
{/snippet}

<Page title={$t("memory.title")}>
  {#snippet actions()}
    {#if draft === null}<Button size="sm" onclick={() => (draft = "")}><Plus size={16} />{$t("memory.add")}</Button>{/if}
  {/snippet}
  <p class="px-1 text-xs break-all text-muted">{$t("memory.subtitle", { path: memoryQuery.data?.path ?? "" })}</p>
  {#if draft !== null}
    <div class="space-y-2 rounded-lg border border-border bg-surface p-4">
      <div class="flex flex-wrap gap-2" role="group" aria-label={$t("memory.kindLabel")}>
        {#each memoryKinds as option (option)}
          <Button size="sm" variant={kind === option ? "default" : "outline"} aria-pressed={kind === option} onclick={() => (kind = option)}>{$t(`memory.kind.${option}`)}</Button>
        {/each}
      </div>
      <Textarea rows={2} bind:value={draft} placeholder={$t("memory.addPlaceholder")} {@attach (node) => node.focus()} />
      <div class="flex justify-end gap-2">
        <Button variant="outline" size="sm" onclick={() => (draft = null)}>{$t("common.cancel")}</Button>
        <Button size="sm" disabled={add.isPending || draftValue.trim().length === 0} onclick={() => add.mutate(draftValue.trim())}>{$t("memory.save")}</Button>
      </div>
    </div>
  {/if}
  {#if memoryQuery.isLoading}
    <p class="flex items-center gap-2 px-1 text-sm text-muted"><span class="h-3 w-3 animate-spin rounded-full border-2 border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {/if}
  {#if !memoryQuery.isLoading && entries.length === 0 && workItems.length === 0 && draft === null}
    <div class="flex flex-col items-center gap-3 py-16">
      <p class="text-sm text-muted">{$t("memory.empty")}</p>
      <Button size="sm" variant="outline" onclick={() => (draft = "")}><Plus size={14} />{$t("memory.add")}</Button>
    </div>
  {/if}
  {#if entries.length > 0}
    {#if workItems.length > 0}<h2 class="px-1 pt-2 text-xs font-medium text-muted">{$t("memory.global")}</h2>{/if}
    {@render entryList(entries)}
  {/if}
  {#each workItems as layer (layer.workItemId)}
    <section class="space-y-2 pt-2" aria-label={$t("memory.workItem", { title: layer.title })}>
      <div class="flex flex-wrap items-baseline gap-2 px-1">
        <h2 class="text-xs font-medium text-muted">{$t("memory.workItem", { title: layer.title })}</h2>
        <span class="font-mono text-xs text-muted">{layer.workItemId}</span>
        <span class="basis-full text-xs text-muted">{$t("memory.workItemHint")}</span>
      </div>
      {@render entryList(layer.entries)}
    </section>
  {/each}
</Page>
