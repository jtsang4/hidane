<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { ChevronDown, ChevronRight } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import ArtifactRow from "./ArtifactRow.svelte";
  import Card from "./ui/Card.svelte";

  let { workItemId }: { workItemId: string } = $props();
  let open = $state(false);
  const filesQuery = createQuery(() => ({
    queryKey: ["files", workItemId],
    queryFn: () => api.workItemFiles(workItemId),
  }));
  let files = $derived(filesQuery.data?.files ?? []);
</script>

<Card class="p-3">
  <button class="flex w-full items-center gap-2 text-left text-sm" aria-expanded={open} onclick={() => (open = !open)}>
    {#if open}<ChevronDown size={16} />{:else}<ChevronRight size={16} />{/if}
    <span class="font-medium">{$t("item.files")}</span>
    <span class="text-xs text-muted">({files.length})</span>
  </button>
  {#if open}
    <div class="mt-2 border-t border-border pt-2">
      <p class="pb-2 text-xs text-muted">{$t("item.filesHint")}</p>
      {#if files.length === 0}
        <p class="text-xs text-muted">{$t("item.filesEmpty")}</p>
      {/if}
      {#each files as file (file.path)}
        <ArtifactRow {workItemId} {file} />
      {/each}
    </div>
  {/if}
</Card>
