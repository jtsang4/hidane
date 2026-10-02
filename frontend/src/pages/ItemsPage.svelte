<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Plus } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type WorkItem } from "../lib/api.js";
  import { atPointer, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { plainClick } from "../lib/nav.js";
  import { matchesItem } from "../lib/search.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
  import { openTaskMenu } from "../lib/taskActions.js";
  import { ui } from "../lib/ui.svelte.js";
  import MoreButton from "../components/MoreButton.svelte";
  import Page from "../components/Page.svelte";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Input from "../components/ui/Input.svelte";
  import Time from "../components/Time.svelte";

  const queryClient = useQueryClient();
  let all = $state(false);
  let query = $state("");

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
    {#if query.trim()}<span class="text-xs text-muted">{$t("items.matched", { n: shown.length, total: items.length })}</span>{/if}
    <Button variant="outline" size="sm" aria-pressed={all} onclick={() => (all = !all)}>{$t(all ? "items.onlyOpen" : "items.showArchived")}</Button>
    <Button size="sm" onclick={() => (ui.newTaskOpen = true)}><Plus size={16} />{$t("items.new")}</Button>
  {/snippet}
  <Input bind:value={query} placeholder={$t("items.search")} aria-label={$t("items.search")} />
  {#if itemsQuery.isLoading}
    <p class="flex items-center gap-2 px-1 text-sm text-muted"><span class="h-3 w-3 animate-spin rounded-full border-2 border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {:else if items.length > 0}
    <ul class="divide-y divide-border rounded-lg border border-border bg-surface">
      {#each shown as item (item.id)}
        <li class="group relative">
          <a
            href={focusHref(item.id)}
            class="flex items-center gap-3 px-4 py-3 pr-12 hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary"
            onclick={(event) => openItem(item.id, event)}
            oncontextmenu={(event) => { event.preventDefault(); menu(item, atPointer(event)); }}
          >
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium">{item.title}</div>
              <div class="mt-0.5 truncate text-xs text-muted">{item.id} · {$t("items.updated")} <Time iso={item.updatedAt} /></div>
            </div>
            {#if running.has(item.id)}
              <Badge tone="default"><span class="mr-1 inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-primary"></span>{$t("items.working")}</Badge>
            {/if}
            <Badge tone={item.status === "open" ? "success" : "muted"}>{$t(`items.status.${item.status}`)}</Badge>
          </a>
          <MoreButton
            class="absolute top-1/2 right-3 -translate-y-1/2 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
            label={$t("menu.moreFor", { title: item.title })}
            onopen={(placement) => menu(item, placement)}
          />
        </li>
      {/each}
    </ul>
    {#if shown.length === 0}<p class="py-10 text-center text-sm text-muted">{$t("items.noMatch")}</p>{/if}
  {:else}
    <div class="flex flex-col items-center gap-3 py-16">
      <p class="text-sm text-muted">{$t("items.empty")}</p>
      <Button size="sm" variant="outline" onclick={() => (ui.newTaskOpen = true)}><Plus size={14} />{$t("shell.newTask")}</Button>
    </div>
  {/if}
</Page>
