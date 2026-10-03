<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { t } from "../../i18n/index.js";
  import { api, type HidaneEvent } from "../../lib/api.js";
  import { matchesQuery } from "../../lib/search.js";
  import { nextCursor } from "../../lib/pagination.js";
  import EventRow from "../../components/EventRow.svelte";
  import { SearchX } from "@lucide/svelte";
  import EmptyState from "../../components/EmptyState.svelte";
  import Button from "../../components/ui/Button.svelte";
  import Input from "../../components/ui/Input.svelte";

  const PAGE_SIZE = 50;
  let kind = $state("");
  let item = $state("");
  let query = $state("");
  let olderPages = $state<HidaneEvent[][]>([]);
  let loadingMore = $state(false);
  let exhausted = $state(false);
  let seenNewestSeq = $state<number | null>(null);

  const pageQuery = createQuery(() => ({
    queryKey: ["events", "page", kind, item],
    queryFn: () => api.eventsPage({ kind, item, limit: PAGE_SIZE }),
  }));
  let newest = $derived([...(pageQuery.data?.events ?? [])].reverse());
  let older = $derived(olderPages.flatMap((page) => [...page].reverse()));
  let loaded = $derived([...newest, ...older]);
  let rows = $derived(query.trim() ? loaded.filter((event) => matchesQuery(event, query)) : loaded);
  let hasMore = $derived((pageQuery.data?.hasMore ?? false) && !exhausted);
  let filtering = $derived(query.trim().length > 0);
  let newestSeq = $derived(newest[0]?.seq ?? null);
  let freshCount = $derived(
    seenNewestSeq !== null && newestSeq !== null && newestSeq > seenNewestSeq
      ? newest.filter((event) => event.seq > (seenNewestSeq as number)).length
      : 0,
  );

  $effect(() => {
    void kind;
    void item;
    olderPages = [];
    exhausted = false;
    seenNewestSeq = null;
  });

  $effect(() => {
    if (newestSeq !== null && seenNewestSeq === null) seenNewestSeq = newestSeq;
  });

  async function loadMore(): Promise<void> {
    const oldestLoaded = nextCursor(pageQuery.data?.events ?? [], olderPages);
    if (oldestLoaded === undefined || loadingMore) return;
    loadingMore = true;
    try {
      const page = await api.eventsPage({ kind, item, before: oldestLoaded, limit: PAGE_SIZE });
      if (page.events.length > 0) olderPages = [...olderPages, page.events];
      if (!page.hasMore || page.events.length === 0) exhausted = true;
    } finally {
      loadingMore = false;
    }
  }
</script>

<div class="space-y-2">
  <Input bind:value={query} placeholder={$t("events.search")} aria-label={$t("events.search")} />
  <div class="flex flex-wrap gap-2">
    <Input class="min-w-0 flex-1" bind:value={kind} placeholder={$t("events.filterKind")} aria-label={$t("events.filterKind")} />
    <Input class="min-w-0 flex-1" bind:value={item} placeholder={$t("events.filterItem")} aria-label={$t("events.filterItem")} />
    {#if kind || item || query}<Button variant="secondary" class="shrink-0" onclick={() => { kind = ""; item = ""; query = ""; }}>{$t("events.clear")}</Button>{/if}
  </div>
  <p class="text-xs text-muted" role="status">{$t("events.showing", { n: loaded.length })}{#if filtering}{` ${$t("events.filtered", { n: rows.length })}`}{/if}{#if freshCount > 0}{` · ${$t("events.fresh", { n: freshCount })}`}{/if}</p>
</div>
{#if rows.length > 0}
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    {#each rows as event (event.id)}<EventRow {event} />{/each}
  </div>
{:else if !pageQuery.isLoading}
  <EmptyState icon={SearchX} text={$t("events.empty")} class="py-12" />
{/if}
{#if loaded.length > 0}
  <div class="pt-2 text-center">
    {#if hasMore}<Button variant="secondary" onclick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? $t("common.loading") : $t("events.loadMore")}</Button>{:else}<span class="text-xs text-muted">{$t("events.allLoaded")}</span>{/if}
  </div>
{/if}
