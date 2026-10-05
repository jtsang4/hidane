<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import Archive from "@lucide/svelte/icons/archive";
  import FolderGit2 from "@lucide/svelte/icons/folder-git-2";
  import GitBranch from "@lucide/svelte/icons/git-branch";
  import MessageSquare from "@lucide/svelte/icons/message-square";
  import Trash2 from "@lucide/svelte/icons/trash";
  import i18n, { t } from "../i18n/index.js";
  import { api, type CheckoutView, type Repo } from "../lib/api.js";
  import { confirmAction } from "../lib/confirm.svelte.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
  import { errorText } from "../lib/settings.js";
  import { pushToast } from "../lib/toast.js";
  import { checkoutBadges, conflictOf } from "../lib/worktrees.js";
  import EmptyState from "./EmptyState.svelte";
  import PathText from "./PathText.svelte";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";

  let { all = false }: { all?: boolean } = $props();

  const queryClient = useQueryClient();
  const checkoutsQuery = createQuery(() => ({
    queryKey: ["checkouts", all],
    queryFn: () => api.checkouts(all),
  }));
  // Listing the repositories checks each one is still there.
  const reposQuery = createQuery(() => ({
    queryKey: ["repos"],
    queryFn: () => api.repos(),
  }));
  let checkouts = $derived(checkoutsQuery.data?.checkouts ?? []);
  let repos = $derived(reposQuery.data?.repos ?? []);
  let busy = $state<string | null>(null);

  function refresh(): void {
    void queryClient.invalidateQueries({ queryKey: ["checkouts"] });
    void queryClient.invalidateQueries({ queryKey: ["repos"] });
    void queryClient.invalidateQueries({ queryKey: ["board"] });
  }

  async function archive(c: CheckoutView): Promise<void> {
    const inPlace = c.mode === "in_place";
    // Only a worktree git can still read is deleted; the server keeps the rest.
    const deletes = !inPlace && c.health === "ok";
    const ok = await confirmAction({
      title: i18n.t("worktrees.archiveTitle"),
      body: inPlace
        ? i18n.t("worktrees.releaseBody")
        : deletes
          ? i18n.t("worktrees.archiveBody", { path: c.path, branch: c.branch })
          : i18n.t("worktrees.keepBody", { path: c.path }),
      confirmLabel: i18n.t("worktrees.archive"),
      destructive: deletes,
    });
    if (!ok) return;
    busy = c.id;
    try {
      await archiveOnce(c);
    } finally {
      busy = null;
      refresh();
    }
  }

  async function archiveOnce(c: CheckoutView, force = false): Promise<void> {
    try {
      await api.archiveCheckout(c.id, force);
      pushToast(i18n.t("worktrees.archived"), "default");
    } catch (failure) {
      const conflict = conflictOf(failure);
      if (conflict?.dirty !== undefined && !force) {
        // Work that was never committed is lost with the directory: asked separately.
        const again = await confirmAction({
          title: i18n.t("worktrees.dirtyTitle", { count: conflict.dirty }),
          body: i18n.t("worktrees.dirtyBody"),
          confirmLabel: i18n.t("worktrees.archiveAnyway"),
          destructive: true,
        });
        if (again) await archiveOnce(c, true);
        return;
      }
      pushToast(conflict?.running ? i18n.t("worktrees.runningRefused") : errorText(failure));
    }
  }

  async function forget(r: Repo): Promise<void> {
    const ok = await confirmAction({
      title: i18n.t("worktrees.forgetTitle", { name: r.name }),
      body: i18n.t("worktrees.forgetBody"),
      confirmLabel: i18n.t("worktrees.forget"),
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.forgetRepo(r.id);
      pushToast(i18n.t("worktrees.forgotten"), "default");
    } catch (failure) {
      pushToast(conflictOf(failure)?.inUse ? i18n.t("worktrees.forgetInUse") : errorText(failure));
    } finally {
      refresh();
    }
  }
</script>

{#if checkoutsQuery.isLoading}
  <p class="flex items-center gap-2 text-sm text-muted"><span class="size-3 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent"></span>{$t("common.loading")}</p>
{:else if checkouts.length > 0}
  <ul class="divide-y divide-border rounded-lg border border-border bg-surface" aria-label={$t("worktrees.tab")}>
    {#each checkouts as c (c.id)}
      {@const badges = checkoutBadges(c)}
      <li class="flex items-start gap-3 px-3.5 py-2.5" data-checkout={c.id}>
        <GitBranch size={14} class="mt-0.5 shrink-0 text-muted" aria-hidden="true" />
        <div class="min-w-0 flex-1 space-y-0.5">
          <div class="flex flex-wrap items-center gap-1.5">
            <span class="text-sm font-medium">{c.repoName}</span>
            <span class="truncate font-mono text-xs text-muted">{c.mode === "in_place" ? c.head || c.branch : c.branch}</span>
            {#if c.mode === "worktree" && c.base && c.base !== c.branch && c.base.startsWith("hidane/")}<span class="truncate font-mono text-2xs text-muted">{$t("worktrees.continues", { branch: c.base })}</span>{/if}
            {#if c.mode === "in_place"}<Badge tone="muted">{$t("worktrees.inPlace")}</Badge>{/if}
            {#each badges as badge (badge.key)}
              <Badge tone={badge.tone}>
                {#if badge.key === "running"}<span class="inline-block size-1.5 animate-ember rounded-full bg-primary"></span>{/if}
                {$t(`worktrees.status.${badge.key}`)}
              </Badge>
            {/each}
          </div>
          <a href={focusHref(c.workItemId)} class="block truncate text-xs text-foreground/80 hover:text-foreground hover:underline" onclick={(event) => { event.preventDefault(); navigate(focusHref(c.workItemId)); }}>{c.title || c.workItemId}</a>
          <p class="text-2xs break-words text-muted"><PathText path={c.path} /></p>
          {#if c.status === "active" && c.health === "ok"}
            <p class="flex flex-wrap gap-x-3 text-2xs text-muted tabular-nums">
              {#if c.ahead > 0}<span>{$t("worktrees.ahead", { count: c.ahead })}</span>{/if}
              <span class={c.dirty > 0 ? "text-foreground/80" : ""}>{c.dirty > 0 ? $t("worktrees.dirty", { count: c.dirty }) : $t("worktrees.clean")}</span>
              <span>{$t("worktrees.activity")} <Time iso={c.lastActivityAt} /></span>
            </p>
          {/if}
        </div>
        <div class="flex shrink-0 items-center gap-0.5">
          <Button variant="ghost" size="icon-sm" aria-label={$t("worktrees.openTask")} title={$t("worktrees.openTask")} onclick={() => navigate(focusHref(c.workItemId))}><MessageSquare size={14} /></Button>
          {#if c.status === "active"}
            <Button variant="ghost" size="icon-sm" aria-label={$t("worktrees.archive")} title={$t("worktrees.archive")} disabled={busy === c.id} onclick={() => archive(c)}><Archive size={14} /></Button>
          {/if}
        </div>
      </li>
    {/each}
  </ul>
{:else}
  <EmptyState icon={GitBranch} text={$t("worktrees.empty")} class="py-10" />
{/if}

<section class="space-y-2" aria-labelledby="repos-heading">
  <h2 id="repos-heading" class="text-sm font-medium">{$t("worktrees.repos")}</h2>
  {#if repos.length > 0}
    <ul class="divide-y divide-border rounded-lg border border-border bg-surface">
      {#each repos as r (r.id)}
        <li class="group flex items-start gap-3 px-3.5 py-2.5" data-repo={r.id}>
          <FolderGit2 size={14} class="mt-0.5 shrink-0 text-muted" aria-hidden="true" />
          <div class="min-w-0 flex-1 space-y-0.5">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-sm font-medium">{r.name}</span>
              {#if r.status === "missing"}<Badge tone="danger">{$t("worktrees.repoMissing")}</Badge>{/if}
              {#if r.defaultBranch}<span class="text-2xs text-muted">{$t("worktrees.defaultBranch", { branch: r.defaultBranch })}</span>{/if}
            </div>
            <p class="text-2xs break-words text-muted"><PathText path={r.path} /></p>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            class="shrink-0 hover:text-danger"
            aria-label={$t("worktrees.forget")}
            title={$t("worktrees.forget")}
            onclick={() => forget(r)}
          ><Trash2 size={14} /></Button>
        </li>
      {/each}
    </ul>
  {:else if !reposQuery.isLoading}
    <p class="text-xs text-muted">{$t("worktrees.reposEmpty")}</p>
  {/if}
</section>
