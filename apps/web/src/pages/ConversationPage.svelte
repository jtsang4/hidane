<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { tick, untrack } from "svelte";
  import { SvelteMap } from "svelte/reactivity";
  import { ArrowDown, UserRound } from "@lucide/svelte";
  import i18n, { language, t } from "../i18n/index.js";
  import { api, ApiError, type BoardCard, type HidaneEvent } from "../lib/api.js";
  import { loadSeen, saveSeen, trayCards } from "../lib/board.js";
  import { buildTurns, type Turn } from "../lib/conversation.js";
  import { awayFromLatest, contextBoundary, dayBreaks, loadedRange } from "../lib/history.js";
  import { liveRepliesFor, maxSeq } from "../lib/liveText.js";
  import { addNotice, dropNotices, noticeFor, type Notice } from "../lib/notices.js";
  import { atFrom, conversationHref, focusFrom, focusHref, navigate, routerState } from "../lib/router.svelte.js";
  import { isPinnedToBottom } from "../lib/scroll.js";
  import { pushToast } from "../lib/toast.js";
  import { cn, fmtDay } from "../lib/utils.js";
  import ChatBubble from "../components/ChatBubble.svelte";
  import Composer, { type ComposerTarget } from "../components/Composer.svelte";
  import DayPicker from "../components/DayPicker.svelte";
  import FocusPanel from "../components/FocusPanel.svelte";
  import NoticeBar from "../components/NoticeBar.svelte";
  import SearchResults from "../components/SearchResults.svelte";
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
  let bottomSentinel = $state<HTMLDivElement | undefined>();
  let composer = $state<Composer | undefined>();
  let loadingOlder = $state(false);
  let loadingNewer = $state(false);
  /** Whether history older than what is loaded exists; null until a page has said. */
  let olderKnown = $state<boolean | null>(null);
  /**
   * `live`: the newest page is loaded and the view follows new events.
   * `window`: a stretch of history opened by a jump (search hit, link, day).
   * It grows both ways as the reader scrolls and rejoins the live edge once a
   * newer page reaches it; until then new events are not mixed into it, since
   * the gap between the two would read as a continuous conversation.
   */
  let mode = $state<"live" | "window">("live");
  let hasNewer = $state(false);
  let personOnly = $state(false);
  let query = $state("");
  /** The query as last settled, which the results follow. */
  let settled = $state("");
  /** A search the reader left by opening a result, offered back to them. */
  let returnQuery = $state<string | null>(null);
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
   * A jump is landing. Paging waits: a window opens scrolled to its top, and
   * an older page prepended then would pin the view there instead of at the
   * event that was asked for.
   */
  let anchoring = $state(false);
  /** The `?at=` last acted on; plain on purpose, it must not drive effects. */
  let handledAt: string | null = null;

  /**
   * Every conversation event loaded into the view, keyed by id. In live mode
   * the newest page slides forward on each refetch while older pages keep
   * their cursor; retaining the union is sound because the log is append-only.
   */
  const seen = new SvelteMap<string, HidaneEvent>();
  /** Work item titles the server sent along with pages, for items no board shows any more. */
  const titles = new SvelteMap<string, string>();

  const conversationQuery = createQuery(() => ({
    queryKey: ["conversation", "page", personOnly],
    queryFn: () => api.eventsPage({ conversation: true, personOnly, limit: PAGE_SIZE }),
  }));
  const boardQuery = createQuery(() => ({
    queryKey: ["board"],
    queryFn: () => api.board(),
  }));
  const contextQuery = createQuery(() => ({
    queryKey: ["conversation", "context"],
    queryFn: () => api.conversationContext(),
  }));

  function absorb(page: { events: HidaneEvent[]; titles?: Record<string, string> }): void {
    for (const event of page.events) seen.set(event.id, event);
    for (const [id, title] of Object.entries(page.titles ?? {})) titles.set(id, title);
  }

  $effect(() => {
    const page = conversationQuery.data;
    if (!page) return;
    if (untrack(() => mode) === "live") absorb(page);
    else for (const [id, title] of Object.entries(page.titles ?? {})) titles.set(id, title);
  });

  // Debounced: every keystroke is a query over the whole log.
  $effect(() => {
    const next = query.trim();
    const timer = window.setTimeout(() => (settled = next), 250);
    return () => window.clearTimeout(timer);
  });

  let focus = $derived(focusFrom(routerState.search));
  let all = $derived([...seen.values()].sort((a, b) => a.seq - b.seq));
  let turns = $derived(buildTurns(all));
  let searching = $derived(query.trim().length > 0);
  let cards = $derived(boardQuery.data?.cards ?? []);
  let cardMap = $derived(new Map(cards.map((card) => [card.item.id, card])));
  /** Titles for items no longer on the board (closed long ago), from what was said about them. */
  let knownTitles = $derived.by(() => {
    const known = new Map<string, string>();
    for (const turn of turns) {
      const title = turn.attribution?.payload["title"];
      if (turn.attribution?.workItemId && typeof title === "string") known.set(turn.attribution.workItemId, title);
    }
    return known;
  });
  let choices = $derived(cards.filter((card) => card.item.status === "open").map((card) => ({ id: card.item.id, title: card.item.title })));
  let tray = $derived(trayCards(cards, seenSeq));
  /** Primary replies still being written. */
  let live = $derived(liveRepliesFor("main", maxSeq(all)));
  let showOptimistic = $derived(mode === "live" && optimistic !== null && !(optimistic.messageId !== null && seen.has(optimistic.messageId)));
  let hasOlder = $derived(olderKnown ?? conversationQuery.data?.hasMore ?? false);
  let breaks = $derived(dayBreaks(turns));
  let showLatest = $derived(awayFromLatest({ mode, follow, searching, primed, hasTurns: turns.length > 0 }));
  let boundary = $derived(contextBoundary(turns, contextQuery.data?.fromId ?? null, hasOlder));
  let target = $derived<ComposerTarget | null>(
    replyTarget ?? (focus ? { id: focus, title: titleOf(focus), mode: "focus" } : null),
  );

  function titleOf(id: string): string {
    return cardMap.get(id)?.item.title ?? titles.get(id) ?? knownTitles.get(id) ?? id;
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
    // Only the live edge can be followed: the bottom of a window is not "now".
    follow = mode === "live" && isPinnedToBottom(el);
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

  /** Scroll only the conversation to an element and mark its turn for a moment. */
  function reveal(el: HTMLElement, root: string, behavior: ScrollBehavior = "smooth"): void {
    const view = viewport;
    if (!view || view.offsetParent === null) return;
    // scrollIntoView would also move every scrollable ancestor, shifting the app shell.
    const offset = el.getBoundingClientRect().top - view.getBoundingClientRect().top;
    view.scrollTo({ top: view.scrollTop + offset - view.clientHeight / 4, behavior });
    highlighted = root;
    window.setTimeout(() => {
      if (highlighted === root) highlighted = null;
    }, 2400);
  }

  /** The turn holding an event, by any of the events it is made of. */
  function turnOf(id: string): Turn | undefined {
    return turns.find(
      (turn) =>
        turn.root === id ||
        turn.message?.id === id ||
        turn.attribution?.id === id ||
        turn.ambiguous?.id === id ||
        turn.answers.some((answer) => answer.id === id),
    );
  }

  function revealLoaded(id: string, behavior: ScrollBehavior = "smooth"): boolean {
    const turn = turnOf(id);
    const el = document.getElementById(`ev-${id}`) ?? (turn ? document.getElementById(`turn-${turn.root}`) : null);
    if (!el || !turn) return false;
    follow = false;
    reveal(el, turn.root, behavior);
    return true;
  }

  /**
   * Open the conversation at one event, however old: go there if it is loaded,
   * otherwise replace the view with a window of history around it.
   */
  async function openAt(id: string): Promise<void> {
    if (seen.has(id)) {
      await tick();
      if (revealLoaded(id)) return;
    }
    try {
      let page = await api.eventsPage({ conversation: true, personOnly, around: id, limit: PAGE_SIZE });
      // The filter may hide exactly what was asked for (an answer to a webhook).
      if (personOnly && !page.events.some((event) => event.id === id)) {
        personOnly = false;
        page = await api.eventsPage({ conversation: true, around: id, limit: PAGE_SIZE });
      }
      follow = false;
      anchoring = true;
      mode = "window";
      seen.clear();
      absorb(page);
      olderKnown = page.hasMore;
      hasNewer = page.hasNewer ?? false;
      if (!hasNewer) rejoinLive();
      await tick();
      // Markdown and cards settle after the first paint.
      window.requestAnimationFrame(() => {
        if (!revealLoaded(id, "instant")) pushToast(i18n.t("chat.notFound"));
        window.setTimeout(() => (anchoring = false), 300);
      });
    } catch (error) {
      anchoring = false;
      pushToast(error instanceof ApiError && error.status === 404 ? i18n.t("chat.notFound") : error instanceof Error ? error.message : String(error));
    }
  }

  /** The window has reached the newest event: it is the live view again. */
  function rejoinLive(): void {
    mode = "live";
    hasNewer = false;
    const page = conversationQuery.data;
    if (page) absorb(page);
    void queryClient.invalidateQueries({ queryKey: ["conversation", "page"] });
  }

  /**
   * Drop any window and show the newest page, pinned to the bottom. With
   * `refill` false the page is left to arrive: after a filter change the
   * cached one still holds the old filter's events.
   */
  function backToLatest(refill = true): void {
    if (atFrom(routerState.search)) navigate(focusHref(focus));
    mode = "live";
    hasNewer = false;
    olderKnown = null;
    seen.clear();
    const page = conversationQuery.data;
    if (page && refill) absorb(page);
    follow = true;
    void queryClient.invalidateQueries({ queryKey: ["conversation", "page"] });
    void tick().then(() => viewport?.scrollTo(0, viewport.scrollHeight));
  }

  /**
   * Back to the newest message. In the live view the loaded history is kept
   * and the view just scrolls down; a window of older history is replaced.
   */
  function goLatest(): void {
    if (mode === "window") {
      backToLatest();
      return;
    }
    if (atFrom(routerState.search)) navigate(focusHref(focus));
    follow = true;
    viewport?.scrollTo({ top: viewport.scrollHeight, behavior: "smooth" });
  }

  $effect(() => {
    const at = atFrom(routerState.search);
    if (!at) {
      handledAt = null;
      return;
    }
    if (at === handledAt) return;
    handledAt = at;
    untrack(() => void openAt(at));
  });

  function jump(notice: Notice): void {
    notices = notices.filter((n) => n.root !== notice.root);
    if (!revealLoaded(notice.root)) goTo(notice.root);
  }

  async function loadOlder(): Promise<void> {
    if (loadingOlder || !hasOlder) return;
    const cursor = loadedRange(seen.values())?.oldest;
    if (cursor === undefined) return;
    const el = viewport;
    // Distance from the bottom survives a prepend; scrollTop does not.
    const fromBottom = el ? el.scrollHeight - el.scrollTop : 0;
    loadingOlder = true;
    try {
      const page = await api.eventsPage({ conversation: true, personOnly, before: cursor, limit: PAGE_SIZE });
      absorb(page);
      olderKnown = page.hasMore && page.events.length > 0;
      await tick();
      if (el) el.scrollTop = el.scrollHeight - fromBottom;
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    } finally {
      loadingOlder = false;
    }
  }

  async function loadNewer(): Promise<void> {
    if (loadingNewer || mode !== "window" || !hasNewer) return;
    const cursor = loadedRange(seen.values())?.newest;
    if (cursor === undefined) return;
    loadingNewer = true;
    try {
      const page = await api.eventsPage({ conversation: true, personOnly, after: cursor, limit: PAGE_SIZE });
      absorb(page);
      hasNewer = page.hasNewer ?? false;
      if (!hasNewer) rejoinLive();
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    } finally {
      loadingNewer = false;
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
    seen.size;
    if (!sentinel || !root || !hasOlder || !primed || searching || anchoring) return;
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

  /** Scroll-down paging inside a window of history, toward the live edge. */
  $effect(() => {
    const sentinel = bottomSentinel;
    const root = viewport;
    seen.size;
    if (!sentinel || !root || mode !== "window" || !hasNewer || searching || anchoring) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) void loadNewer();
      },
      { root, rootMargin: "0px 0px 300px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  });

  function setPersonOnly(next: boolean): void {
    if (next === personOnly) return;
    personOnly = next;
    backToLatest(false);
  }

  /** Open the conversation at an event through the URL, so it can be linked and gone back from. */
  function goTo(id: string): void {
    if (atFrom(routerState.search) === id) void openAt(id);
    else navigate(conversationHref({ at: id, focus }));
  }

  function openHit(event: HidaneEvent): void {
    returnQuery = query.trim();
    query = "";
    settled = "";
    goTo(event.id);
  }

  function backToSearch(): void {
    if (returnQuery) query = returnQuery;
    returnQuery = null;
  }

  function openFocus(id: string): void {
    navigate(conversationHref({ at: atFrom(routerState.search), focus: id }));
  }

  function closeFocus(): void {
    replyTarget = null;
    navigate(conversationHref({ at: atFrom(routerState.search), focus: null }));
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

  async function copyLink(messageId: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}${conversationHref({ at: messageId })}`);
      pushToast(i18n.t("chat.linkCopied"), "default");
    } catch (error) {
      pushToast(error instanceof Error ? error.message : String(error));
    }
  }

  async function hide(messageId: string): Promise<void> {
    if (!confirm(i18n.t("chat.hideConfirm"))) return;
    try {
      await api.redactMessage(messageId);
      const event = seen.get(messageId);
      if (event) {
        const payload: Record<string, unknown> = { ...event.payload, text: "", redacted: true };
        delete payload["images"];
        delete payload["imageCount"];
        seen.set(messageId, { ...event, payload });
      }
      pushToast(i18n.t("chat.hideDone"), "default");
      void queryClient.invalidateQueries({ queryKey: ["conversation"] });
      void queryClient.invalidateQueries({ queryKey: ["conversation-search"] });
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
  {@const day = breaks.get(turn.root)}
  {#if day}
    {#key $language}
      <div class="flex items-center gap-3 pt-2 text-[11px] text-muted" role="separator" aria-label={fmtDay(day)}>
        <span class="h-px flex-1 bg-border"></span><span>{fmtDay(day)}</span><span class="h-px flex-1 bg-border"></span>
      </div>
    {/key}
  {/if}
  {#if boundary === turn.root}
    <p class="rounded-md border border-dashed border-border px-3 py-1.5 text-center text-[11px] text-muted" role="note">{$t("chat.contextBoundary")}</p>
  {/if}
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
    onlink={(id) => void copyLink(id)}
    onhide={(id) => void hide(id)}
  />
{/snippet}

<div class="flex h-full flex-col">
  <TaskTray cards={tray} seen={seenSeq} focused={focus} onfocus={openFocus} />
  <div class="flex min-h-0 flex-1">
    <div class={cn("relative flex min-w-0 flex-1 flex-col", focus && "hidden md:flex")}>
      <div class="border-b border-border p-2">
        <div class="mx-auto flex max-w-3xl items-center gap-1.5">
          <Input
            bind:value={query}
            type="search"
            placeholder={$t("chat.search")}
            aria-label={$t("chat.search")}
            onkeydown={(event) => { if (event.key === "Escape") query = ""; }}
          />
          <DayPicker onpick={goTo} />
          <button
            class={cn("flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border px-2.5 text-xs focus-visible:outline-2 focus-visible:outline-primary", personOnly ? "border-primary/60 bg-primary/10 text-foreground" : "border-border text-muted hover:text-foreground")}
            aria-pressed={personOnly}
            aria-label={$t("chat.personOnly")}
            title={$t("chat.personOnlyHint")}
            onclick={() => setPersonOnly(!personOnly)}
          >
            <UserRound size={14} aria-hidden="true" /><span class="hidden sm:inline" aria-hidden="true">{$t("chat.personOnly")}</span>
          </button>
        </div>
        {#if !searching && returnQuery}
          <div class="mx-auto max-w-3xl pt-1">
            <button class="px-1 text-xs text-primary hover:underline" onclick={backToSearch}>← {$t("chat.searchBack", { query: returnQuery })}</button>
          </div>
        {/if}
      </div>
      {#if mode === "window" && !searching}
        <div class="border-b border-border bg-surface-2/60 px-3 py-1.5 text-center text-xs text-muted" role="status">
          {$t("chat.viewingHistory")}
        </div>
      {/if}
      <!-- `relative` keeps absolutely positioned descendants (screen-reader text,
           menus) inside the scroller; otherwise they overflow the page shell and
           the whole app scrolls with the conversation. -->
      <div bind:this={viewport} onscroll={onScroll} class="relative flex-1 overflow-y-auto p-4">
        {#if searching}
          <div class="mx-auto max-w-3xl">
            {#if settled}
              <SearchResults query={settled} {titleOf} onopen={openHit} onfocus={openFocus} />
            {:else}
              <p class="px-1 text-xs text-muted">{$t("chat.searching")}</p>
            {/if}
          </div>
        {/if}
        <!-- Hidden, not unmounted, until the first pin: the height must be real
             for the observer to measure it (collapsing it here would mean the
             observer never fires and the first pin never happens). While
             searching it collapses so the results start at the top. -->
        <div bind:this={content} class={cn("mx-auto max-w-3xl space-y-5", turns.length > 0 && !primed && "invisible", searching && "invisible h-0 overflow-hidden", showLatest && "pb-10")}>
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
          {#each turns as turn (turn.root)}{@render turnView(turn)}{/each}
          {#if mode === "window" && hasNewer}
            <div class="text-center">
              <Button variant="outline" size="sm" onclick={() => void loadNewer()} disabled={loadingNewer}>
                {loadingNewer ? $t("common.loading") : $t("chat.loadNewer")}
              </Button>
            </div>
          {/if}
          {#if showOptimistic && optimistic}
            {@const ghost = { seq: 0, id: "optimistic", ts: new Date().toISOString(), source: "connector:web", kind: "user.message", threadId: "main", workItemId: null, executionId: null, payload: { text: optimistic.text } } satisfies HidaneEvent}
            <ChatBubble event={ghost} ghost />
          {/if}
          {#if mode === "live"}
            {#each live as reply (reply.id)}
              {@const liveEvent = { seq: 0, id: reply.id, ts: reply.ts, source: "agent:primary", kind: "agent.reply", threadId: reply.threadId, workItemId: null, executionId: null, payload: { text: reply.text } } satisfies HidaneEvent}
              <ChatBubble event={liveEvent} streaming={!reply.done} />
            {/each}
          {/if}
          <div bind:this={bottomSentinel} aria-hidden="true"></div>
        </div>
      </div>
      <!-- Just above the composer: the way back to the newest message, then
           any off-screen updates. Stacked so neither covers the other. -->
      <div class="pointer-events-none absolute inset-x-0 bottom-2 z-10 flex flex-col items-center gap-2 px-3">
        {#if showLatest}
          <button
            class="pointer-events-auto flex items-center gap-1.5 rounded-full border border-border bg-surface/95 px-3 py-1.5 text-xs text-foreground shadow-lg backdrop-blur hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary"
            onclick={goLatest}
          >
            <ArrowDown size={14} aria-hidden="true" />{$t("chat.backToLatest")}
          </button>
        {/if}
        <NoticeBar {notices} {titleOf} onjump={jump} ondismiss={() => (notices = [])} />
      </div>
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
      // Something new said belongs at the live edge, not inside old history.
      if (mode === "window" || searching) {
        query = "";
        backToLatest();
      }
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
