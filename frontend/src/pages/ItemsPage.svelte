<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import Archive from "@lucide/svelte/icons/archive";
  import ListTodo from "@lucide/svelte/icons/list-todo";
  import Plus from "@lucide/svelte/icons/plus";
  import SearchX from "@lucide/svelte/icons/search-x";
  import { t } from "../i18n/index.js";
  import { api, type WorkItem } from "../lib/api.js";
  import { atPointer, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { plainClick } from "../lib/nav.js";
  import { matchesItem } from "../lib/search.js";
  import { focusHref, itemsHref, itemsViewFrom, navigate, type ItemsView } from "../lib/router.svelte.js";
  import { openTaskMenu } from "../lib/taskActions.js";
  import { ui } from "../lib/ui.svelte.js";
  import { segment, segmented, segmentOff, segmentOn, toolbarButton, toolbarButtonOn } from "../lib/styles.js";
  import { cn } from "../lib/utils.js";
  import MoreButton from "../components/MoreButton.svelte";
  import EmptyState from "../components/EmptyState.svelte";
  import Page from "../components/Page.svelte";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Input from "../components/ui/Input.svelte";
  import Time from "../components/Time.svelte";
  import WorktreesView from "../components/WorktreesView.svelte";

  const queryClient = useQueryClient();
  let all = $state(false);
  let query = $state("");
  let view = $derived(itemsViewFrom());
  const views: ItemsView[] = ["tasks", "worktrees"];

  const itemsQuery = createQuery(() => ({
    queryKey: ["items", all],
    queryFn: () => api.workItems(all),
  }));
  let items = $derived(itemsQuery.data?.items ?? []);
  let running = $derived(new Set(itemsQuery.data?.running ?? []));
  let shown = $derived(query.trim() ? items.filter((item) => matchesItem(item, query)) : items);

  function openItem(id: string, event: MouseEvent): void {
    if (!plainClick(event)) return;
    event.preventDefault();
    navigate(focusHref(id));
  }

  function menu(item: WorkItem, placement: MenuPlacement): void {
    openTaskMenu(queryClient, { id: item.id, title: item.title, running: running.has(item.id), status: item.status }, placement);
  }
</script>

<Page title={$t("items.title")}>
  {#snippet actions()}
    {#if view === "tasks" && query.trim()}<span class="text-xs text-muted">{$t("items.matched", { n: shown.length, total: items.length })}</span>{/if}
    <div class={segmented} role="group" aria-label={$t("items.title")}>
      {#each views as v (v)}
        <button type="button" class={cn(segment, view === v ? segmentOn : segmentOff)} aria-pressed={view === v} onclick={() => navigate(itemsHref(v), { replace: true })}>{$t(v === "tasks" ? "worktrees.tasks" : "worktrees.tab")}</button>
      {/each}
    </div>
    <button type="button" class={cn(toolbarButton, all && toolbarButtonOn)} aria-pressed={all} onclick={() => (all = !all)}><Archive aria-hidden="true" />{$t(all ? "items.onlyOpen" : view === "tasks" ? "items.showArchived" : "worktrees.showArchived")}</button>
    {#if view === "tasks"}<Button variant="soft" onclick={() => (ui.newTaskOpen = true)}><Plus size={16} />{$t("items.new")}</Button>{/if}
  {/snippet}
  {#if view === "worktrees"}
    <WorktreesView {all} />
  {:else}
  <Input bind:value={query} placeholder={$t("items.search")} aria-label={$t("items.search")} />
  {#if itemsQuery.isLoading}
    <p class="flex items-center gap-2 text-sm text-muted"><span class="size-3 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {:else if items.length > 0}
    {#if shown.length > 0}
    <ul class="divide-y divide-border rounded-lg border border-border bg-surface">
      {#each shown as item (item.id)}
        <li class="group relative">
          <a
            href={focusHref(item.id)}
            class="flex items-center gap-3 px-3.5 py-2.5 transition-colors [@media(hover:none)]:pr-11 duration-100 hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary/70"
            onclick={(event) => openItem(item.id, event)}
            oncontextmenu={(event) => { event.preventDefault(); menu(item, atPointer(event)); }}
          >
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium">{item.title}</div>
              <div class="mt-0.5 truncate text-xs text-muted">{item.id} · {$t("items.updated")} <Time iso={item.updatedAt} /></div>
            </div>
            <!-- With a pointer, the row's menu button takes the badges' place on hover. -->
            <div class="flex shrink-0 items-center gap-1.5 transition-opacity [@media(hover:hover)]:group-focus-within:opacity-0 [@media(hover:hover)]:group-hover:opacity-0">
              {#if running.has(item.id)}
                <Badge tone="default"><span class="inline-block size-1.5 animate-ember rounded-full bg-primary"></span>{$t("items.working")}</Badge>
              {/if}
              <!-- Open is the ordinary state and goes unmarked: only an ending earns a badge. -->
              {#if item.status !== "open"}
                <Badge tone={item.status === "done" ? "success" : "muted"}>{$t(`items.status.${item.status}`)}</Badge>
              {/if}
            </div>
          </a>
          <MoreButton
            class="absolute top-1/2 right-3 -translate-y-1/2 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
            label={$t("menu.moreFor", { title: item.title })}
            onopen={(placement) => menu(item, placement)}
          />
        </li>
      {/each}
    </ul>
    {:else}
    <EmptyState icon={SearchX} text={$t("items.noMatch")} class="py-10" />
    {/if}
  {:else}
    <EmptyState icon={ListTodo} text={$t("items.empty")}>
      <Button variant="secondary" onclick={() => (ui.newTaskOpen = true)}><Plus />{$t("shell.newTask")}</Button>
    </EmptyState>
  {/if}
  {/if}
</Page>
