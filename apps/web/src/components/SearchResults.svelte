<script lang="ts">
  import { createInfiniteQuery } from "@tanstack/svelte-query";
  import { t } from "../i18n/index.js";
  import { api, type HidaneEvent, type WorkItem } from "../lib/api.js";
  import { excerpt, highlight, saidText, searchTerms } from "../lib/history.js";
  import { fmtDateTime } from "../lib/utils.js";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";

  let {
    query,
    titleOf,
    onopen,
    onfocus,
  }: {
    /** Settled query text; typing is debounced by the caller. */
    query: string;
    titleOf: (id: string) => string;
    onopen: (event: HidaneEvent) => void;
    onfocus: (id: string) => void;
  } = $props();

  const results = createInfiniteQuery(() => ({
    queryKey: ["conversation-search", query],
    queryFn: ({ pageParam }: { pageParam: number | undefined }) => api.searchConversation(query, pageParam),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last: Awaited<ReturnType<typeof api.searchConversation>>) =>
      last.hasMore ? last.events.at(-1)?.seq : undefined,
    enabled: query.length > 0,
  }));

  let terms = $derived(searchTerms(query));
  let pages = $derived(results.data?.pages ?? []);
  let hits = $derived(pages.flatMap((page) => page.events));
  let items = $derived<WorkItem[]>(pages[0]?.items ?? []);
  let titles = $derived(Object.assign({}, ...pages.map((page) => page.titles)) as Record<string, string>);

  function who(event: HidaneEvent): string {
    if (event.kind === "user.message") return $t("chat.who.person");
    if (event.kind === "escalation") return $t("chat.who.question");
    return $t("chat.who.assistant");
  }

  function itemTitle(id: string): string {
    return titles[id] ?? titleOf(id);
  }
</script>

<div class="space-y-4" aria-busy={results.isFetching}>
  <p class="px-1 text-xs text-muted" role="status" aria-live="polite">
    {#if results.isPending}{$t("chat.searching")}{:else}{$t(results.hasNextPage ? "chat.searchMatchesMore" : "chat.searchMatches", { n: hits.length + items.length })}{/if}
  </p>

  {#if items.length > 0}
    <section class="space-y-1.5">
      <h2 class="px-1 text-xs font-medium text-muted">{$t("chat.searchItems")}</h2>
      <ul class="space-y-1">
        {#each items as item (item.id)}
          <li>
            <button class="flex w-full items-center gap-2 rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-surface-2" onclick={() => onfocus(item.id)}>
              <span class="min-w-0 flex-1 truncate">
                {#each highlight(item.title, terms) as part, i (i)}{#if part.hit}<mark class="rounded-sm bg-primary/25 text-foreground">{part.text}</mark>{:else}{part.text}{/if}{/each}
              </span>
              <Badge tone="muted">{item.status === "open" ? $t("items.working") : $t(`task.state.${item.status}`)}</Badge>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if hits.length > 0}
    <section class="space-y-1.5">
      <h2 class="px-1 text-xs font-medium text-muted">{$t("chat.searchMessages")}</h2>
      <ul class="space-y-1">
        {#each hits as hit (hit.id)}
          <li>
            <button class="w-full rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-surface-2" onclick={() => onopen(hit)}>
              <span class="flex items-center gap-2 text-[11px] text-muted">
                <span class="font-medium text-foreground/80">{who(hit)}</span>
                {#if hit.workItemId}<span class="min-w-0 truncate">· {itemTitle(hit.workItemId)}</span>{/if}
                <time class="ml-auto shrink-0" dateTime={hit.ts}>{fmtDateTime(hit.ts)}</time>
              </span>
              <span class="mt-1 block break-words">
                {#each highlight(excerpt(saidText(hit), terms), terms) as part, i (i)}{#if part.hit}<mark class="rounded-sm bg-primary/25 text-foreground">{part.text}</mark>{:else}{part.text}{/if}{/each}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if results.isSuccess && hits.length === 0 && items.length === 0}
    <p class="pt-8 text-center text-sm text-muted">{$t("chat.searchEmpty")}</p>
  {/if}
  {#if results.isError}
    <p class="pt-8 text-center text-sm text-danger">{results.error?.message}</p>
  {/if}
  {#if results.hasNextPage}
    <div class="text-center">
      <Button variant="outline" size="sm" onclick={() => void results.fetchNextPage()} disabled={results.isFetchingNextPage}>
        {results.isFetchingNextPage ? $t("common.loading") : $t("chat.searchMore")}
      </Button>
    </div>
  {/if}
</div>
