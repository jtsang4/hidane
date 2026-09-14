<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { ChevronDown, ChevronRight, Download, FileText } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, authHeaders, type ArtifactEntry } from "../lib/api.js";
  import Time from "./Time.svelte";
  import Button from "./ui/Button.svelte";
  import Markdown from "./Markdown.svelte";

  let { workItemId, file }: { workItemId: string; file: ArtifactEntry } = $props();
  let open = $state(false);
  const contentQuery = createQuery(() => ({
    queryKey: ["artifact", workItemId, file.path],
    queryFn: () => api.workItemFile(workItemId, file.path),
    enabled: open,
  }));

  function humanSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }

  async function download(): Promise<void> {
    const res = await fetch(
      `/api/work-items/${workItemId}/file?download&path=${encodeURIComponent(file.path)}`,
      { headers: authHeaders() },
    );
    if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = file.path.split("/").pop() ?? "file";
    anchor.click();
    URL.revokeObjectURL(url);
  }
</script>

<div class="border-b border-border py-2 last:border-b-0">
  <div class="flex items-center gap-2">
    <button class="flex min-w-0 flex-1 items-center gap-2 text-left text-sm" aria-expanded={open} onclick={() => (open = !open)}>
      {#if open}<ChevronDown size={16} />{:else}<ChevronRight size={16} />{/if}
      <FileText size={16} class="shrink-0 text-muted" />
      <span class="truncate font-mono text-xs">{file.path}</span>
    </button>
    <span class="shrink-0 text-xs text-muted">{humanSize(file.size)}</span>
    <Button
      variant="ghost"
      size="icon"
      aria-label={`${$t("item.download")} ${file.path}`}
      onclick={() => void download()}
    >
      <Download size={16} />
    </Button>
  </div>
  {#if open}
    <div class="mt-2 space-y-1">
      <p class="text-xs text-muted"><Time iso={file.modifiedAt} /></p>
      {#if contentQuery.isLoading}<p class="text-xs text-muted">{$t("common.loading")}</p>{/if}
      {#if contentQuery.data?.reason === "binary"}<p class="text-xs text-muted">{$t("item.binaryFile")}</p>{/if}
      {#if contentQuery.data?.reason === "too-large"}<p class="text-xs text-muted">{$t("item.tooLarge")}</p>{/if}
      {#if contentQuery.data?.text !== undefined}
        {#if file.path.endsWith(".md")}
          <div class="max-h-96 overflow-auto rounded bg-background p-2 text-xs">
            <Markdown content={contentQuery.data.text} />
          </div>
        {:else}
          <pre class="max-h-96 overflow-auto rounded bg-background p-2 text-xs whitespace-pre-wrap">{contentQuery.data.text}</pre>
        {/if}
      {/if}
    </div>
  {/if}
</div>
