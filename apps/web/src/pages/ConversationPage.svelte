<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { tick } from "svelte";
  import { SvelteMap } from "svelte/reactivity";
  import i18n, { t } from "../i18n/index.js";
  import { api, type BoardCard, type HidaneEvent } from "../lib/api.js";
  import { loadSeen, saveSeen, trayCards } from "../lib/board.js";
  import { buildTurns, type Turn } from "../lib/conversation.js";
  import { liveRepliesFor, maxSeq } from "../lib/liveText.js";
  import { addNotice, dropNotices, noticeFor, type Notice } from "../lib/notices.js";
  import { nextCursor } from "../lib/pagination.js";
  import { focusFrom, focusHref, navigate, routerState } from "../lib/router.svelte.js";
  import { isPinnedToBottom } from "../lib/scroll.js";
  import { matchesQuery } from "../lib/search.js";
  import { pushToast } from "../lib/toast.js";
  import { cn } from "../lib/utils.js";
  import ChatBubble from "../components/ChatBubble.svelte";
  import Composer, { type ComposerTarget } from "../components/Composer.svelte";
  import FocusPanel from "../components/FocusPanel.svelte";
  import NoticeBar from "../components/NoticeBar.svelte";
  import TaskTray from "../components/TaskTray.svelte";
  import TurnGroup from "../components/TurnGroup.svelte";
  import Button from "../components/ui/Button.svelte";
  import Input from "../components/ui/Input.svelte";

  /** Events per request; older pages load as the reader scrolls up. */
  const PAGE_SIZE = 80;

  const queryClient = useQueryClient();
  let viewport = $state<HTMLDivElement | undefined>();
  let content = $state<HTMLDivElement | undefined>();
  let topSentinel = $state<HTMLDivElement | undefined>();
  let composer = $state<Composer | undefined>();
  /** Cursor bookkeeping only — rendering reads the union in `seen`. */
  let olderPages = $state<HidaneEvent[][]>([]);
  let loadingOlder = $state(false);
  let exhausted = $state(false);
  let query = $state("");
  /** At the live edge and wanting to stay there. */
  let follow = $state(true);
  /** The first page has been pinned to the live edge; paging waits for it. */
  let primed = $state(false);
  let optimistic = $state<{ text: string; messageId: string | null } | null>(null);
  /** Answering a specific question; outranks the focused card as the target. */
  let replyTarget = $state<ComposerTarget | null>(null);
  let notices = $state<Notice[]>([]);
  let highlighted = $state<string | null>(null);
  let seenSeq = $state<Record<string, number>>(loadSeen());

  /**
   * Every conversation event this session has observed, keyed by id. The
   * newest page slides forward on each refetch while older pages keep their
   * cursor; retaining the union is sound because the log is append-only.
   */
  const seen = new SvelteMap<string, HidaneEvent>();

  const conversationQuery = createQuery(() => ({
    queryKey: ["conversation"],
    queryFn: () => api.eventsPage({ conversation: true, limit: PAGE_SIZE }),
  }));
  const boardQuery = createQuery(() => ({
    queryKey: ["board"],
    queryFn: () => api.board(),
  }));

  $effect(() => {
    const page = conversationQuery.data?.events;
    if (!page) return;
    for (const event of page) seen.set(event.id, event);
  });

  let focus = $derived(focusFrom(routerState.search));
  let all = $derived([...seen.values()].sort((a, b) => a.seq - b.seq));
  let turns = $derived(buildTurns(all));
  let searching = $derived(query.trim().length > 0);
  let visibleTurns = $derived(
    searching
      ? turns.filter((turn) => [turn.message, ...turn.answers].some((e) => e !== null && matchesQuery(e, query)))
      : turns,
  );
  let cards = $derived(boardQuery.data?.cards ?? []);
  let cardMap = $derived(new Map(cards.map((card) => [card.item.id, card])));
  /** Titles for items no longer on the board (closed long ago), from what was said about them. */
  let knownTitles = $derived.by(() => {
    const titles = new Map<string, string>();
    for (const turn of turns) {
      const title = turn.attribution?.payload["title"];
      if (turn.attribution?.workItemId && typeof title === "string") titles.set(turn.attribution.workItemId, title);
    }
    return titles;
  });
  let choices = $derived(cards.filter((card) => card.item.status === "open").map((card) => ({ id: card.item.id, title: card.item.title })));
  let tray = $derived(trayCards(cards, seenSeq));
  /** Primary replies still being written. */
  let live = $derived(liveRepliesFor("main", maxSeq(all)));
  let showOptimistic = $derived(optimistic !== null && !(optimistic.messageId !== null && seen.has(optimistic.messageId)));
  let hasOlder = $derived((conversationQuery.data?.hasMore ?? false) && !exhausted);
  let target = $derived<ComposerTarget | null>(
    replyTarget ?? (focus ? { id: focus, title: titleOf(focus), mode: "focus" } : null),
  );

  function titleOf(id: string): string {
    return cardMap.get(id)?.item.title ?? knownTitles.get(id) ?? id;
  }

  /**
   * Unread is "new since you last looked". A card seen for the first time sets
   * its own baseline; opening a card moves the baseline to now.
   */
  function refreshSeen(): void {
    let changed = false;
    const next = { ...seenSeq };
    for (const card of cards) {
      const baseline = next[card.item.id];
      // Looking at the card in the conversation counts as having seen it.
      const inView = card.anchor !== null && primed && turnVisible(card.anchor, false);
      if (baseline === undefined || ((card.item.id === focus || inView) && baseline < card.lastSeq)) {
        next[card.item.id] = card.lastSeq;
        changed = true;
      }
    }
    if (changed) {
      seenSeq = next;
      saveSeen(next);
    }
  }

  $effect(() => {
    refreshSeen();
  });

  /**
   * Follow the live edge by watching rendered height, not the data: content is
   * not in the DOM when the data changes and keeps settling afterwards, so only
   * a layout observer lands the jump at the real bottom. Instant on purpose.
   */
  $effect(() => {
    const el = viewport;
    const inner = content;
    if (!el || !inner) return;
    const observer = new ResizeObserver(() => {
      if (turns.length === 0 && !showOptimistic) return;
      if (follow && !searching) el.scrollTo(0, el.scrollHeight);
      primed = true;
    });
    observer.observe(inner);
    return () => observer.disconnect();
  });

  /** Whether a turn's end (where new answers land) — or with `whole` false, any of it — is on screen. */
  function turnVisible(root: string, whole = true): boolean {
    const el = document.getElementById(`turn-${root}`);
    const view = viewport;
    if (!el || !view || view.offsetParent === null) return false;
    const r = el.getBoundingClientRect();
    const v = view.getBoundingClientRect();
    return whole ? r.bottom > v.top + 24 && r.bottom <= v.bottom + 8 : r.bottom > v.top && r.top < v.bottom;
  }

  function onScroll(): void {
    const el = viewport;
    if (!el) return;
    follow = isPinnedToBottom(el);
    refreshSeen();
    if (notices.length > 0) {
      notices = dropNotices(notices, new Set(notices.filter((n) => turnVisible(n.root)).map((n) => n.root)));
    }
  }

  /**
   * An answer that lands off-screen — under an older message, or while the
   * reader is scrolled away — leaves a line at the bottom instead of moving
   * the view. Judged after the refetch has rendered it.
   */
  $effect(() => {
    const onEvent = (raw: Event) => {
      let event: HidaneEvent;
      try {
        event = JSON.parse((raw as MessageEvent<string>).data) as HidaneEvent;
      } catch {
        return;
      }
      const notice = noticeFor(event);
      if (!notice) return;
      if (notice.workItemId && notice.workItemId === focusFrom(routerState.search)) return;
      window.setTimeout(() => {
        if (!turnVisible(notice.root)) notices = addNotice(notices, notice);
      }, 900);
    };
    window.addEventListener("hidane:event", onEvent);
    return () => window.removeEventListener("hidane:event", onEvent);
  });

  function jump(notice: Notice): void {
    notices = notices.filter((n) => n.root !== notice.root);
    const el = document.getElementById(`turn-${notice.root}`);
    const view = viewport;
    if (el && view && view.offsetParent !== null) {
      // Scroll only the conversation: scrollIntoView also moves every scrollable
      // ancestor, which shifts the whole app shell.
      const offset = el.getBoundingClientRect().top - view.getBoundingClientRect().top;
      view.scrollTo({ top: view.scrollTop + offset - view.clientHeight / 4, behavior: "smooth" });
      highlighted = notice.root;
      window.setTimeout(() => {
        if (highlighted === notice.root) highlighted = null;
      }, 2000);
    } else if (notice.workItemId) {
      openFocus(notice.workItemId);
    }
  }

  async function loadOlder(): Promise<void> {
    if (loadingOlder || exhausted) return;
    const cursor = nextCursor(conversationQuery.data?.events ?? [], olderPages);
    if (cursor === undefined) return;
    const el = viewport;
    // Distance from the bottom survives a prepend; scrollTop does not.
    const fromBottom = el ? el.scrollHeight - el.scrollTop : 0;
    loadingOlder = true;
    try {
      const page = await api.eventsPage({ conversation: true, before: cursor, limit: PAGE_SIZE });
      if (page.events.length > 0) {
        olderPages = [...olderPages, page.events];
        for (const event of page.events) seen.set(event.id, event);
      }
      if (!page.hasMore || page.events.length === 0) exhausted = true;
      await tick();
      if (el) el.scrollTop = el.scrollHeight - fromBottom;
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    } finally {
      loadingOlder = false;
    }
  }

  /**
   * Scroll-up paging, held off until the first pin, while searching, and in a
   * conversation too short to scroll (the sentinel would stay in view and walk
   * the whole history). Re-armed per page.
   */
  $effect(() => {
    const sentinel = topSentinel;
    const root = viewport;
    olderPages.length;
    if (!sentinel || !root || !hasOlder || !primed || searching) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        if (root.scrollHeight <= root.clientHeight) return;
        void loadOlder();
      },
      { root, rootMargin: "300px 0px 0px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  });

  function openFocus(id: string): void {
    navigate(focusHref(id));
  }

  function closeFocus(): void {
    replyTarget = null;
    navigate(focusHref(null));
  }

  function answer(card: BoardCard): void {
    replyTarget = { id: card.item.id, title: card.item.title, mode: "reply", replyTo: card.escalation?.id };
    composer?.focusInput();
  }

  function answerEscalation(eventId: string, workItemId: string): void {
    replyTarget = { id: workItemId, title: titleOf(workItemId), mode: "reply", replyTo: eventId };
    composer?.focusInput();
  }

  async function stop(id: string): Promise<void> {
    if (!confirm($t("task.confirmStop"))) return;
    try {
      await api.cancelExecution(id);
      pushToast(i18n.t("task.stopped"), "default");
      void queryClient.invalidateQueries({ queryKey: ["board"] });
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    }
  }

  async function route(messageId: string, workItemId: string): Promise<void> {
    try {
      await api.routeMessage(messageId, workItemId);
      pushToast(i18n.t("attribution.moved"), "default");
      void queryClient.invalidateQueries({ queryKey: ["conversation"] });
      void queryClient.invalidateQueries({ queryKey: ["board"] });
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    }
  }

  function clearTarget(): void {
    if (replyTarget) replyTarget = null;
    else closeFocus();
  }
</script>

{#snippet turnView(turn: Turn)}
  <TurnGroup
    {turn}
    cards={cardMap}
    {choices}
    {titleOf}
    focused={focus}
    highlighted={highlighted === turn.root}
    onroute={(messageId, workItemId) => void route(messageId, workItemId)}
    onfocus={openFocus}
    onanswer={answer}
    onanswerEscalation={answerEscalation}
    onstop={(id) => void stop(id)}
  />
{/snippet}

<div class="flex h-full flex-col">
  <TaskTray cards={tray} seen={seenSeq} focused={focus} onfocus={openFocus} />
  <div class="flex min-h-0 flex-1">
    <div class={cn("relative flex min-w-0 flex-1 flex-col", focus && "hidden md:flex")}>
      <div class="border-b border-border p-2">
        <div class="mx-auto max-w-3xl">
        <Input bind:value={query} placeholder={$t("chat.search")} aria-label={$t("chat.search")} />
        {#if searching}
          <p class="px-1 pt-1 text-xs text-muted">
            {$t("chat.searchMatches", { n: visibleTurns.length })}{#if hasOlder}<span class="px-1">·</span>{$t("chat.searchPartial")}{/if}
          </p>
        {/if}
        </div>
      </div>
      <!-- `relative` keeps absolutely positioned descendants (screen-reader text,
           menus) inside the scroller; otherwise they overflow the page shell and
           the whole app scrolls with the conversation. -->
      <div bind:this={viewport} onscroll={onScroll} class="relative flex-1 overflow-y-auto p-4">
        <!-- Hidden, not unmounted, until the first pin: the height must be real
             for the observer to measure it. -->
        <div bind:this={content} class={cn("mx-auto max-w-3xl space-y-5", turns.length > 0 && !primed && "invisible")}>
          <div bind:this={topSentinel} aria-hidden="true"></div>
          {#if hasOlder}
            <div class="text-center">
              <Button variant="outline" size="sm" onclick={() => void loadOlder()} disabled={loadingOlder}>
                {loadingOlder ? $t("common.loading") : $t("chat.loadEarlier")}
              </Button>
            </div>
          {:else if turns.length > 0}
            <p class="text-center text-xs text-muted">{$t("chat.historyStart")}</p>
          {/if}
          {#if turns.length === 0 && !showOptimistic && conversationQuery.data}<p class="pt-16 text-center text-sm text-muted">{$t("conversation.empty")}</p>{/if}
          {#if searching && visibleTurns.length === 0 && turns.length > 0}<p class="pt-8 text-center text-sm text-muted">{$t("chat.searchEmpty")}</p>{/if}
          {#each visibleTurns as turn (turn.root)}{@render turnView(turn)}{/each}
          {#if !searching}
            {#if showOptimistic && optimistic}
              {@const ghost = { seq: 0, id: "optimistic", ts: new Date().toISOString(), source: "connector:web", kind: "user.message", threadId: "main", workItemId: null, executionId: null, payload: { text: optimistic.text } } satisfies HidaneEvent}
              <ChatBubble event={ghost} ghost />
            {/if}
            {#each live as reply (reply.id)}
              {@const liveEvent = { seq: 0, id: reply.id, ts: reply.ts, source: "agent:primary", kind: "agent.reply", threadId: reply.threadId, workItemId: null, executionId: null, payload: { text: reply.text } } satisfies HidaneEvent}
              <ChatBubble event={liveEvent} streaming={!reply.done} />
            {/each}
          {/if}
        </div>
      </div>
      <NoticeBar {notices} {titleOf} onjump={jump} ondismiss={() => (notices = [])} />
    </div>
    {#if focus}
      <div class="flex min-h-0 w-full flex-col border-border md:w-[46%] md:max-w-2xl md:border-l">
        {#key focus}
          <FocusPanel id={focus} cards={cardMap} onclose={closeFocus} onfocus={openFocus} onanswer={answer} onstop={(id) => void stop(id)} />
        {/key}
      </div>
    {/if}
  </div>
  <Composer
    bind:this={composer}
    {target}
    onclear={clearTarget}
    onsending={(text) => {
      optimistic = { text, messageId: null };
      follow = true;
    }}
    onsent={(messageId, sentTo) => {
      if (optimistic) optimistic = { ...optimistic, messageId };
      if (sentTo?.mode === "reply") replyTarget = null;
      void queryClient.invalidateQueries({ queryKey: ["conversation"] });
    }}
    onfailed={() => (optimistic = null)}
  />
</div>
