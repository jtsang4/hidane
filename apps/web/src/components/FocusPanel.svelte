<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Archive, ArrowUpLeft, Check, RotateCcw, Square, X } from "@lucide/svelte";
  import { SvelteMap } from "svelte/reactivity";
  import { t } from "../i18n/index.js";
  import { api, ApiError, type BoardCard, type HidaneEvent, type WorkItemStatus } from "../lib/api.js";
  import { stateTone } from "../lib/board.js";
  import { executionGroups } from "../lib/grouping.js";
  import { liveRepliesFor, maxSeq } from "../lib/liveText.js";
  import { nextCursor } from "../lib/pagination.js";
  import { pushToast } from "../lib/toast.js";
  import { fmtDateTime } from "../lib/utils.js";
  import Artifacts from "./Artifacts.svelte";
  import ChatBubble from "./ChatBubble.svelte";
  import Execution from "./Execution.svelte";
  import TaskCard from "./TaskCard.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";

  let {
    id,
    cards,
    onclose,
    onfocus,
    onanswer,
    onstop,
  }: {
    id: string;
    cards: ReadonlyMap<string, BoardCard>;
    onclose: () => void;
    onfocus: (id: string) => void;
    onanswer: (card: BoardCard) => void;
    onstop: (id: string) => void;
  } = $props();

  /** A busy item emits executions and side effects far faster than messages,
   *  so its history is paged rather than fetched whole. */
  const PAGE_SIZE = 150;
  const THREAD_KINDS = new Set(["agent.reply", "agent.error", "execution.steered"]);

  const queryClient = useQueryClient();
  let olderPages = $state<HidaneEvent[][]>([]);
  let loadingOlder = $state(false);
  let exhausted = $state(false);
  /** Append-only union across the sliding newest page and older pages. */
  const seen = new SvelteMap<string, HidaneEvent>();

  const itemQuery = createQuery(() => ({
    queryKey: ["item", id],
    queryFn: () => api.workItem(id, PAGE_SIZE),
    retry: false,
  }));
  let data = $derived(itemQuery.data);
  let card = $derived(cards.get(id));

  $effect(() => {
    const page = itemQuery.data?.events;
    if (!page) return;
    for (const event of page) seen.set(event.id, event);
  });

  let events = $derived([...seen.values()].filter((e) => e.workItemId === id).sort((a, b) => a.seq - b.seq));
  /** What was said to and by this item: the Manager's copies of the person's
   *  words (main-thread originals are the conversation's, not repeated here). */
  let thread = $derived(events.filter((e) => (e.kind === "user.message" && e.threadId !== "main") || THREAD_KINDS.has(e.kind)));
  let executions = $derived(executionGroups(events));
  let live = $derived(liveRepliesFor(data?.item.threadId ?? "", maxSeq(thread)));
  let hasOlder = $derived((data?.hasMore ?? false) && !exhausted);
  let parent = $derived(data?.item.parentId ? cards.get(data.item.parentId) : undefined);

  async function loadOlder(): Promise<void> {
    if (loadingOlder || exhausted) return;
    const cursor = nextCursor(itemQuery.data?.events ?? [], olderPages);
    if (cursor === undefined) return;
    loadingOlder = true;
    try {
      const page = await api.eventsPage({ item: id, before: cursor, limit: PAGE_SIZE });
      if (page.events.length > 0) {
        olderPages = [...olderPages, page.events];
        for (const event of page.events) seen.set(event.id, event);
      }
      if (!page.hasMore || page.events.length === 0) exhausted = true;
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    } finally {
      loadingOlder = false;
    }
  }

  const setStatus = createMutation<{ ok: boolean; item: unknown }, unknown, WorkItemStatus>(() => ({
    mutationFn: (status) => api.setWorkItemStatus(id, status),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["item", id] });
      void queryClient.invalidateQueries({ queryKey: ["board"] });
      void queryClient.invalidateQueries({ queryKey: ["items"] });
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));
</script>

<aside class="flex h-full min-h-0 flex-col bg-background" aria-label={data?.item.title ?? id}>
  {#if itemQuery.error}
    <div class="flex items-center justify-between border-b border-border p-3">
      <p class="text-sm text-muted">{$t("item.notFound")}</p>
      <Button variant="ghost" size="icon" aria-label={$t("task.close")} onclick={onclose}><X size={16} /></Button>
    </div>
  {:else if !data}
    <p class="p-4 text-sm text-muted">{$t("common.loading")}</p>
  {:else}
    {@const item = data.item}
    <header class="border-b border-border p-3">
      <div class="flex items-start gap-2">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-base font-semibold break-words">{item.title}</h2>
            {#if card}<Badge tone={stateTone(card.state)}>{$t(`task.state.${card.state}`)}</Badge>{:else}<Badge tone="muted">{item.status}</Badge>{/if}
          </div>
          <p class="mt-1 truncate text-xs text-muted" title={item.workspace}>{$t("item.meta", { id: item.id, time: fmtDateTime(item.createdAt), workspace: item.workspace })}</p>
          {#if item.parentId}
            <button class="mt-1 flex items-center gap-1 text-xs text-primary hover:underline" onclick={() => item.parentId && onfocus(item.parentId)}>
              <ArrowUpLeft size={12} />{$t("task.parent")} · {parent?.item.title ?? item.parentId}
            </button>
          {/if}
        </div>
        <div class="flex shrink-0 items-center gap-1">
          {#if data.running}
            <Button variant="outline" size="sm" aria-label={$t("task.stop")} onclick={() => onstop(id)}><Square size={12} /><span class="hidden sm:inline">{$t("task.stop")}</span></Button>
          {/if}
          <Button variant="outline" size="sm" aria-label={item.status === "open" ? $t("item.markDone") : $t("item.reopen")} disabled={setStatus.isPending} onclick={() => setStatus.mutate(item.status === "open" ? "done" : "open")}>{#if item.status === "open"}<Check size={12} />{:else}<RotateCcw size={12} />{/if}<span class="hidden sm:inline">{item.status === "open" ? $t("item.markDone") : $t("item.reopen")}</span></Button>
          {#if item.status !== "closed"}
            <Button variant="ghost" size="icon" aria-label={$t("item.archive")} title={$t("item.archive")} disabled={setStatus.isPending} onclick={() => { if (confirm($t("item.confirmArchive"))) setStatus.mutate("closed"); }}><Archive size={16} /></Button>
          {/if}
          <Button variant="ghost" size="icon" aria-label={$t("task.close")} title={$t("task.close")} onclick={onclose}><X size={16} /></Button>
        </div>
      </div>
    </header>

    <div class="relative min-h-0 flex-1 space-y-4 overflow-y-auto p-3">
      {#if card && (card.understanding || card.execution || card.escalation || card.childIds.length > 0 || card.lastPolicyBlock)}
        <TaskCard {card} {cards} focused headless {onfocus} {onanswer} {onstop} />
      {/if}

      <section class="space-y-2" aria-label={$t("task.thread")}>
        <h3 class="text-sm font-medium text-muted">{$t("task.thread")}</h3>
        {#if hasOlder}
          <div class="text-center">
            <Button variant="outline" size="sm" onclick={() => void loadOlder()} disabled={loadingOlder}>{loadingOlder ? $t("common.loading") : $t("chat.loadEarlier")}</Button>
          </div>
        {/if}
        {#if thread.length === 0 && live.length === 0}<p class="text-xs text-muted">{$t("task.noThread")}</p>{/if}
        {#each thread as event (event.id)}
          {#if event.kind === "execution.steered"}
            <p class="text-right text-xs text-muted">↳ {$t("conversation.steered")}</p>
          {:else}
            <ChatBubble {event} />
          {/if}
        {/each}
        {#each live as reply (reply.id)}
          {@const liveEvent = { seq: 0, id: reply.id, ts: reply.ts, source: "agent:manager", kind: "agent.reply", threadId: reply.threadId, workItemId: id, executionId: null, payload: { text: reply.text } } satisfies HidaneEvent}
          <ChatBubble event={liveEvent} streaming={!reply.done} />
        {/each}
      </section>

      <Artifacts workItemId={id} />

      {#if executions.length > 0}
        <section class="space-y-2">
          <h3 class="text-sm font-medium text-muted">{$t("item.timeline")}</h3>
          {#each [...executions].reverse() as group (group.executionId)}<Execution {group} />{/each}
        </section>
      {/if}
    </div>
  {/if}
</aside>
