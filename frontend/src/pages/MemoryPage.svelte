<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Brain, Plus, Trash2 } from "@lucide/svelte";
  import i18n, { t } from "../i18n/index.js";
  import { api, type MemoryEntry } from "../lib/api.js";
  import { confirmAction } from "../lib/confirm.svelte.js";
  import { pushToast, toastError } from "../lib/toast.js";
  import EmptyState from "../components/EmptyState.svelte";
  import { segment, segmented, segmentOff, segmentOn } from "../lib/styles.js";
  import { cn } from "../lib/utils.js";
  import PathText from "../components/PathText.svelte";
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
    onError: (error) => toastError(error),
  }));
  const add = createMutation(() => ({
    mutationFn: (content: string) => api.addMemory(kind, content),
    onSuccess: () => {
      pushToast(i18n.t("memory.added"), "default");
      draft = null;
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (error) => toastError(error),
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

</script>

{#snippet entryList(list: MemoryEntry[])}
  <ul class="divide-y divide-border rounded-lg border border-border bg-surface">
    {#each list as entry (entry.id)}
      <li class="flex items-start gap-3 px-3.5 py-2.5">
        <!-- What is remembered leads; when and where it came from follows. -->
        <div class="min-w-0 flex-1">
          <p class="text-sm break-words whitespace-pre-wrap select-text">{entry.content}</p>
          <div class="mt-1 flex flex-wrap items-center gap-2">
            <Badge tone="muted">{$t(`memory.kind.${entry.kind}`)}</Badge>
            <span class="text-xs text-muted tabular-nums">{entry.date}</span>
            <span class="font-mono text-2xs text-muted">{entry.id}</span>
          </div>
        </div>
        <Button variant="ghost" size="icon" class="-my-0.5 hover:bg-danger/10 hover:text-danger" aria-label={`${$t("memory.forget")} ${entry.id}`} disabled={forget.isPending} onclick={() => void confirmForget(entry)}><Trash2 /></Button>
      </li>
    {/each}
  </ul>
{/snippet}

<Page title={$t("memory.title")}>
  {#snippet actions()}
    {#if draft === null}<Button variant="soft" onclick={() => (draft = "")}><Plus size={16} />{$t("memory.add")}</Button>{/if}
  {/snippet}
  <p class="text-xs break-words text-muted">{#each $t("memory.subtitle", { path: "\u0000" }).split("\u0000") as piece, index (index)}{#if index > 0}<span class="mt-0.5 block font-mono text-2xs select-text"><PathText path={memoryQuery.data?.path ?? ""} /></span>{/if}{piece}{/each}</p>
  {#if draft !== null}
    <div class="space-y-2 rounded-lg border border-border bg-surface p-3">
      <div class={segmented} role="group" aria-label={$t("memory.kindLabel")}>
        {#each memoryKinds as option (option)}
          <button type="button" class={cn(segment, kind === option ? segmentOn : segmentOff)} aria-pressed={kind === option} onclick={() => (kind = option)}>{$t(`memory.kind.${option}`)}</button>
        {/each}
      </div>
      <Textarea rows={2} bind:value={draft} placeholder={$t("memory.addPlaceholder")} {@attach (node) => node.focus()} />
      <div class="flex justify-end gap-2">
        <Button variant="secondary" onclick={() => (draft = null)}>{$t("common.cancel")}</Button>
        <Button disabled={add.isPending || draftValue.trim().length === 0} onclick={() => add.mutate(draftValue.trim())}>{$t("memory.save")}</Button>
      </div>
    </div>
  {/if}
  {#if memoryQuery.isLoading}
    <p class="flex items-center gap-2 text-sm text-muted"><span class="size-3 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {/if}
  {#if !memoryQuery.isLoading && entries.length === 0 && workItems.length === 0 && draft === null}
    <EmptyState icon={Brain} text={$t("memory.empty")}>
      <Button variant="secondary" onclick={() => (draft = "")}><Plus />{$t("memory.add")}</Button>
    </EmptyState>
  {/if}
  {#if entries.length > 0}
    {#if workItems.length > 0}<h2 class="pt-2 text-sm font-medium">{$t("memory.global")}</h2>{/if}
    {@render entryList(entries)}
  {/if}
  {#each workItems as layer (layer.workItemId)}
    <section class="space-y-2 pt-2" aria-label={$t("memory.workItem", { title: layer.title })}>
      <div class="flex flex-wrap items-baseline gap-2">
        <h2 class="text-sm font-medium">{$t("memory.workItem", { title: layer.title })}</h2>
        <span class="font-mono text-xs text-muted">{layer.workItemId}</span>
        <span class="basis-full text-xs text-muted">{$t("memory.workItemHint")}</span>
      </div>
      {@render entryList(layer.entries)}
    </section>
  {/each}
</Page>
