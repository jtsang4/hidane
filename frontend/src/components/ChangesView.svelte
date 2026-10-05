<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import GitBranch from "@lucide/svelte/icons/git-branch";
  import GitCommitHorizontal from "@lucide/svelte/icons/git-commit-horizontal";
  import { SvelteSet } from "svelte/reactivity";
  import { t } from "../i18n/index.js";
  import { api, type CheckoutChanges, type FileChange } from "../lib/api.js";
  import { patchLines, patchSections } from "../lib/changes.js";
  import { cn } from "../lib/utils.js";
  import Badge from "./ui/Badge.svelte";

  let { workItemId, heading = true }: { workItemId: string; /** Left out where a toggle already names the section. */ heading?: boolean } = $props();

  const changesQuery = createQuery(() => ({
    queryKey: ["changes", workItemId],
    queryFn: () => api.changes(workItemId),
  }));
  let checkouts = $derived(changesQuery.data?.checkouts ?? []);
  const opened = new SvelteSet<string>();

  function toggle(key: string): void {
    if (opened.has(key)) opened.delete(key);
    else opened.add(key);
  }

  const lineTone = {
    add: "bg-success/10 text-success",
    del: "bg-danger/10 text-danger",
    hunk: "text-muted",
    meta: "text-muted/80",
    context: "text-foreground/80",
  } as const;

  const statusTone = (status: FileChange["status"]) => (status === "deleted" ? "danger" : status === "added" || status === "untracked" ? "success" : "muted");
</script>

{#snippet counts(file: FileChange)}
  {#if file.additions < 0}
    <span class="text-muted">{$t("review.binary")}</span>
  {:else}
    <span class="text-success tabular-nums">+{file.additions}</span><span class="text-danger tabular-nums">−{file.deletions}</span>
  {/if}
{/snippet}

{#snippet repo(change: CheckoutChanges)}
  {@const sections = patchSections(change.patch)}
  <div class="rounded-lg border border-border bg-surface">
    <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-border px-3 py-2 text-xs">
      <GitBranch size={12} class="shrink-0 text-muted" aria-hidden="true" />
      <span class="font-medium">{change.repo}</span>
      <span class="truncate font-mono text-muted">{change.branch}</span>
      <span class="text-muted">· {change.mode === "in_place" ? $t("review.uncommitted") : $t("review.against", { base: change.against })}</span>
      <span class="ml-auto text-muted">{$t("review.files", { count: change.files.length })}{#if change.commits.length > 0} · {$t("review.commits", { count: change.commits.length })}{/if}</span>
    </div>
    {#if change.problem}
      <p class="px-3 py-2 text-xs text-danger">{change.problem === "missing" ? $t("review.missing") : $t("review.repoMissing")}</p>
    {:else}
      {#if change.commits.length > 0}
        <ul class="space-y-0.5 border-b border-border px-3 py-2 text-xs">
          {#each change.commits as commit (commit.hash)}
            <li class="flex min-w-0 items-center gap-1.5"><GitCommitHorizontal size={12} class="shrink-0 text-muted" aria-hidden="true" /><span class="shrink-0 font-mono text-muted">{commit.hash}</span><span class="truncate select-text">{commit.subject}</span></li>
          {/each}
        </ul>
      {/if}
      {#if change.files.length === 0}
        <p class="px-3 py-2 text-xs text-muted">{$t("review.none")}</p>
      {:else}
        <ul class="divide-y divide-border">
          {#each change.files as file (file.path)}
            {@const key = `${change.checkoutId}:${file.path}`}
            {@const section = sections.get(file.path)}
            <li>
              <button
                class="flex w-full min-w-0 items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-accent disabled:hover:bg-transparent"
                aria-expanded={section ? opened.has(key) : undefined}
                aria-label={$t("review.showFile", { path: file.path })}
                disabled={!section}
                onclick={() => toggle(key)}
              >
                <ChevronRight size={12} class={cn("shrink-0 text-muted transition-transform", opened.has(key) && "rotate-90", !section && "invisible")} aria-hidden="true" />
                <span class="min-w-0 flex-1 truncate font-mono">{#if file.from}<span class="text-muted">{file.from} → </span>{/if}{file.path}</span>
                <Badge tone={statusTone(file.status)}>{$t(`review.status.${file.status}`)}</Badge>
                <span class="flex shrink-0 gap-1.5">{@render counts(file)}</span>
              </button>
              {#if section && opened.has(key)}
                <pre class="max-h-96 overflow-auto bg-well py-1 font-mono text-2xs leading-relaxed select-text">{#each patchLines(section) as line, index (index)}<span class={cn("block px-3 whitespace-pre", lineTone[line.kind])}>{line.text || " "}</span>{/each}</pre>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      {#if change.truncated}<p class="border-t border-border px-3 py-1.5 text-2xs text-muted">{$t("review.truncated")}</p>{/if}
    {/if}
  </div>
{/snippet}

{#if checkouts.length > 0}
  <section class="space-y-2" aria-label={$t("review.changes")}>
    {#if heading}<h3 class="text-xs font-medium text-muted">{$t("review.changes")}</h3>{/if}
    {#each checkouts as change (change.checkoutId)}{@render repo(change)}{/each}
  </section>
{/if}
