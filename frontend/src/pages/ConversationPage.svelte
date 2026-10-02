<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { tick, untrack } from "svelte";
  import { SvelteMap } from "svelte/reactivity";
  import { ArrowDown, Copy, EyeOff, ImagePlus, Link, UserRound } from "@lucide/svelte";
  import i18n, { language, t } from "../i18n/index.js";
  import { api, ApiError, type BoardCard, type HidaneEvent } from "../lib/api.js";
  import { boot } from "../lib/boot.js";
  import { confirmAction } from "../lib/confirm.svelte.js";
  import { openMenu, type MenuEntry, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { buildTurns, type Turn } from "../lib/conversation.js";
  import { awayFromLatest, contextBoundary, dayBreaks, loadedRange, mergeHistory, saidText } from "../lib/history.js";
  import { acknowledgeLiveReplies, liveRepliesFor, maxSeq, watchLiveReplies } from "../lib/liveText.js";
  import { messageActions, type MessageAction } from "../lib/menus.js";
  import { copyText } from "../lib/native.js";
  import { addNotice, dropNotices, noticeFor, type Notice } from "../lib/notices.js";
  import { atFrom, conversationHref, focusFrom, focusHref, navigate, routerState } from "../lib/router.svelte.js";
  import { followAfterScroll, isPinnedToBottom, scrollerGesture } from "../lib/scroll.js";
  import { seenState, updateSeen } from "../lib/seen.svelte.js";
  import { errorText } from "../lib/settings.js";
  import { openTaskMenu, stopTask } from "../lib/taskActions.js";
  import { pushToast, toastError } from "../lib/toast.js";
  import { ui } from "../lib/ui.svelte.js";
  import { cn, fmtDay } from "../lib/utils.js";
  import ChatBubble from "../components/ChatBubble.svelte";
  import Composer, { type ComposerTarget } from "../components/Composer.svelte";
  import DayPicker from "../components/DayPicker.svelte";
  import FocusPanel from "../components/FocusPanel.svelte";
  import NoticeBar from "../components/NoticeBar.svelte";
  import Toolbar from "../components/Toolbar.svelte";
  import TurnGroup from "../components/TurnGroup.svelte";
  import Button from "../components/ui/Button.svelte";

  /** Events per request; older pages load as the reader scrolls up. */
  const PAGE_SIZE = 80;

  const queryClient = useQueryClient();
  const desktop = boot().desktop;
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
   * It slides both ways as the reader scrolls and rejoins the live edge once a
   * newer page reaches it; until then new events are not mixed into it, since
   * the gap between the two would read as a continuous conversation.
   */
  let mode = $state<"live" | "window">("live");
  let hasNewer = $state(false);
  let personOnly = $state(false);
  /** At the live edge and wanting to stay there. */
  let follow = $state(true);
  /** When the reader last touched the scroller (wheel, touch, key, pointer); never, at first. */
  let userScrollAt = Number.NEGATIVE_INFINITY;
  /** Image files are being dragged over the conversation. */
  let dragging = $state(false);
  /** The first page has been pinned to the live edge; paging waits for it. */
  let primed = $state(false);
  let optimistic = $state<{ text: string; messageId: string | null } | null>(null);
  /** Answering a specific question; outranks the focused card as the target. */
  let replyTarget = $state<ComposerTarget | null>(null);
  let notices = $state<Notice[]>([]);
  /** Height of what floats over the log's bottom ("back to latest", notices). */
  let overlayHeight = $state(0);
  let highlighted = $state<string | null>(null);
  /**
   * A jump is landing. Paging waits: a window opens scrolled to its top, and
   * an older page prepended then would pin the view there instead of at the
   * event that was asked for.
   */
  let anchoring = $state(false);
  /** The `?at=` last acted on; plain on purpose, it must not drive effects. */
  let handledAt: string | null = null;

  /**
   * A bounded, contiguous window, keyed by id. Evicted events stay addressable
   * through server cursors, search and permalinks.
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

  function absorb(page: { events: HidaneEvent[]; titles?: Record<string, string> }, direction: "older" | "newer" = "newer"): void {
    const merged = mergeHistory(seen.values(), page.events, direction);
    const retained = new Set(merged.events.map((event) => event.id));
    for (const id of seen.keys()) if (!retained.has(id)) seen.delete(id);
    for (const event of merged.events) seen.set(event.id, event);
    for (const [id, title] of Object.entries(page.titles ?? {})) titles.set(id, title);
    const items = new Set(merged.events.map((event) => event.workItemId));
    for (const id of titles.keys()) if (!items.has(id)) titles.delete(id);
    if (merged.droppedOlder) olderKnown = true;
    if (merged.droppedNewer) {
      mode = "window";
      hasNewer = true;
    }
  }

  $effect(() => {
    const page = conversationQuery.data;
    if (!page) return;
    untrack(() => {
      if (mode !== "live") return;
      // Preserve the reader's place when live arrivals would evict it. The
      // newer cursor will fill this gap when they scroll toward the live edge.
      if (!follow && mergeHistory(seen.values(), page.events, "newer").droppedOlder) {
        mode = "window";
        hasNewer = true;
        return;
      }
      absorb(page);
    });
  });

  let focus = $derived(focusFrom(routerState.search));
  let all = $derived([...seen.values()].sort((a, b) => a.seq - b.seq));
  let turns = $derived(buildTurns(all));
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
  /** Primary replies still being written. */
  let live = $derived(liveRepliesFor("main", maxSeq(all)));

  $effect(() => {
    if (mode === "live") return watchLiveReplies("main");
  });

  $effect(() => {
    acknowledgeLiveReplies("main", maxSeq(all));
  });
  let showOptimistic = $derived(mode === "live" && optimistic !== null && !(optimistic.messageId !== null && seen.has(optimistic.messageId)));
  let hasOlder = $derived(olderKnown ?? conversationQuery.data?.hasMore ?? false);
  let breaks = $derived(dayBreaks(turns));
  let showLatest = $derived(awayFromLatest({ mode, follow, searching: false, primed, hasTurns: turns.length > 0 }));
  let boundary = $derived(contextBoundary(turns, contextQuery.data?.fromId ?? null, hasOlder));
  let target = $derived<ComposerTarget | null>(
    replyTarget ?? (focus ? { id: focus, title: titleOf(focus), mode: "focus" } : null),
  );
  /** What the addressed task runs on, for the composer's picker. */
  let targetRunAs = $derived(target ? (cardMap.get(target.id)?.item.runAs ?? null) : null);

  function titleOf(id: string): string {
    return cardMap.get(id)?.item.title ?? titles.get(id) ?? knownTitles.get(id) ?? id;
  }

  /**
   * Unread is "new since you last looked". A card seen for the first time sets
   * its own baseline; opening a card moves the baseline to now.
   */
  function refreshSeen(): void {
    let changed = false;
    const next = { ...seenState.map };
    for (const card of cards) {
      const baseline = next[card.item.id];
      // Looking at the card in the conversation counts as having seen it.
      const inView = card.anchor !== null && primed && turnVisible(card.anchor, false);
      if (baseline === undefined || ((card.item.id === focus || inView) && baseline < card.lastSeq)) {
        next[card.item.id] = card.lastSeq;
        changed = true;
      }
    }
    if (changed) updateSeen(next);
  }

  $effect(() => {
    refreshSeen();
  });

  /**
   * Follow the live edge by watching rendered height, not the data: content is
   * not in the DOM when the data changes and keeps settling afterwards, so only
   * a layout observer lands the jump at the real bottom. Instant on purpose.
   * The scroller itself is watched too: the composer growing (an attachment,
   * a reply target) shortens it without the content changing.
   */
  $effect(() => {
    const el = viewport;
    const inner = content;
    if (!el || !inner) return;
    const observer = new ResizeObserver(() => {
      if (turns.length === 0 && !showOptimistic) return;
      if (follow && mode === "live") el.scrollTo(0, el.scrollHeight);
      // Next frame: what `primed` changes re-lays out the content, and doing that
      // inside the observer's delivery is a "ResizeObserver loop" error in WebKit.
      if (!primed) window.requestAnimationFrame(() => (primed = true));
    });
    observer.observe(inner);
    observer.observe(el);
    return () => observer.disconnect();
  });

  /**
   * Notes the reader's own hand on the scroller; only that may leave the live
   * edge. Only gestures that move the view count: a click, a right-click or a
   * text selection inside the log does not — counting every pointerdown let a
   * reply that landed within a second of opening a message's menu unpin the
   * view for good.
   */
  function watchReader(node: HTMLElement): () => void {
    const touched = () => (userScrollAt = performance.now());
    const onPointer = (event: PointerEvent) => {
      if (scrollerGesture({ onScroller: event.target === node })) touched();
    };
    const onKey = (event: KeyboardEvent) => {
      if (scrollerGesture({ key: event.key })) touched();
    };
    node.addEventListener("wheel", touched, { passive: true });
    node.addEventListener("touchmove", touched, { passive: true });
    node.addEventListener("pointerdown", onPointer, { passive: true });
    node.addEventListener("keydown", onKey, { passive: true });
    return () => {
      node.removeEventListener("wheel", touched);
      node.removeEventListener("touchmove", touched);
      node.removeEventListener("pointerdown", onPointer);
      node.removeEventListener("keydown", onKey);
    };
  }

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
    const decision = followAfterScroll({
      live: mode === "live",
      pinned: isPinnedToBottom(el),
      userInitiated: performance.now() - userScrollAt < 1000,
      following: follow,
    });
    if (decision === "follow") follow = true;
    else if (decision === "unfollow") follow = false;
    else if (decision === "repin") el.scrollTo(0, el.scrollHeight);
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
        // Following the live edge, an answer anywhere on screen is being seen as it lands.
        const seenNow = follow && mode === "live" ? turnVisible(notice.root, false) : turnVisible(notice.root);
        if (!seenNow) notices = addNotice(notices, notice);
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
      titles.clear();
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
    // The permalink was the window's; the live view is not "at" anything.
    if (atFrom(routerState.search)) navigate(focusHref(focusFrom(routerState.search)), { replace: true });
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
    titles.clear();
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

  /** Restore a visible turn after adding a page and evicting the opposite end. */
  function preserveScroll(direction: "older" | "newer"): () => void {
    const el = viewport;
    if (!el) return () => {};
    const anchor = [...el.querySelectorAll<HTMLElement>("section[data-root]")].find((node) => node.getBoundingClientRect().bottom > el.getBoundingClientRect().top);
    const anchorTop = anchor?.getBoundingClientRect().top ?? 0;
    const root = anchor?.dataset["root"];
    const beforeHeight = el.scrollHeight;
    const beforeTop = el.scrollTop;
    return () => {
      const retainedAnchor = root ? document.getElementById(`turn-${root}`) : null;
      el.scrollTop = retainedAnchor
        ? el.scrollTop + retainedAnchor.getBoundingClientRect().top - anchorTop
        : beforeTop + (direction === "older" ? el.scrollHeight - beforeHeight : 0);
    };
  }

  async function loadOlder(): Promise<void> {
    if (loadingOlder || !hasOlder) return;
    const cursor = loadedRange(seen.values())?.oldest;
    if (cursor === undefined) return;
    const restore = preserveScroll("older");
    loadingOlder = true;
    try {
      const page = await api.eventsPage({ conversation: true, personOnly, before: cursor, limit: PAGE_SIZE });
      absorb(page, "older");
      olderKnown = page.hasMore && page.events.length > 0;
      await tick();
      restore();
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
    const restore = preserveScroll("newer");
    loadingNewer = true;
    try {
      const page = await api.eventsPage({ conversation: true, personOnly, after: cursor, limit: PAGE_SIZE });
      absorb(page);
      hasNewer = page.hasNewer ?? false;
      if (!hasNewer) rejoinLive();
      await tick();
      restore();
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
    loadedRange(seen.values())?.oldest;
    if (!sentinel || !root || !hasOlder || !primed || anchoring) return;
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
    loadedRange(seen.values())?.newest;
    if (!sentinel || !root || mode !== "window" || !hasNewer || anchoring) return;
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

  function stop(id: string): void {
    void stopTask(queryClient, id);
  }

  function taskMenu(card: BoardCard, placement: MenuPlacement): void {
    const running = card.execution !== null && (card.state === "running" || card.state === "queued" || card.state === "thinking");
    openTaskMenu(queryClient, { id: card.item.id, title: card.item.title, running, status: card.item.status }, placement, openFocus);
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
      await copyText(`${window.location.origin}${conversationHref({ at: messageId })}`);
      pushToast(i18n.t("chat.linkCopied"), "default");
    } catch (error) {
      toastError(error);
    }
  }

  async function copyMessage(event: HidaneEvent): Promise<void> {
    try {
      await copyText(saidText(event));
      pushToast(i18n.t("menu.textCopied"), "default");
    } catch (error) {
      toastError(error);
    }
  }

  const MESSAGE_ITEMS: Record<MessageAction, { label: "menu.copyText" | "menu.hide" | "menu.copyLink"; icon: MenuEntry["icon"] }> = {
    "copy-text": { label: "menu.copyText", icon: Copy },
    hide: { label: "menu.hide", icon: EyeOff },
    "copy-link": { label: "menu.copyLink", icon: Link },
  };

  function messageMenu(event: HidaneEvent, placement: MenuPlacement): void {
    const own = event.kind === "user.message";
    const redacted = event.payload["redacted"] === true || (own && turnOf(event.id)?.redacted === true);
    const items = messageActions({ desktop, own, redacted }).map((action) => ({
      id: action,
      label: i18n.t(MESSAGE_ITEMS[action].label),
      icon: MESSAGE_ITEMS[action].icon,
      danger: action === "hide",
    }));
    openMenu(placement, i18n.t("menu.messageLabel"), items, (choice) => {
      if (choice === "copy-text") void copyMessage(event);
      else if (choice === "hide") void hide(event.id);
      else void copyLink(event.id);
    });
  }

  async function hide(messageId: string): Promise<void> {
    const confirmed = await confirmAction({
      title: i18n.t("chat.hideConfirmTitle"),
      body: i18n.t("chat.hideConfirm"),
      confirmLabel: i18n.t("menu.hide"),
      destructive: true,
    });
    if (!confirmed) return;
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
    } catch (error) {
      pushToast(errorText(error));
    }
  }

  function clearTarget(): void {
    if (replyTarget) replyTarget = null;
    else closeFocus();
  }

  // ⌘L, the menu's New Message, or the palette: the request outlives the navigation here.
  $effect(() => {
    if (!ui.composerFocus || !composer) return;
    ui.composerFocus = false;
    void tick().then(() => composer?.focusInput());
  });

  const hasFiles = (event: DragEvent) => event.dataTransfer?.types.includes("Files") ?? false;

  function onDragOver(event: DragEvent): void {
    if (!hasFiles(event)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
    dragging = true;
  }

  function onDragLeave(event: DragEvent): void {
    const to = event.relatedTarget;
    if (!(to instanceof Node) || !(event.currentTarget as HTMLElement).contains(to)) dragging = false;
  }

  function onDrop(event: DragEvent): void {
    if (!hasFiles(event)) return;
    event.preventDefault();
    dragging = false;
    const files = [...(event.dataTransfer?.files ?? [])];
    if (files.length > 0) composer?.attachFiles(files);
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
    routingKnown={mode === "live"}
    onroute={(messageId, workItemId) => void route(messageId, workItemId)}
    onfocus={openFocus}
    onanswer={answer}
    onanswerEscalation={answerEscalation}
    onstop={stop}
    onmessagemenu={messageMenu}
    ontaskmenu={taskMenu}
  />
{/snippet}

<div
  class="relative flex h-full flex-col"
  role="region"
  aria-label={$t("nav.chat")}
  ondragenter={onDragOver}
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
>
  <Toolbar title={$t("nav.chat")}>
    {#snippet actions()}
      <DayPicker onpick={goTo} />
      <button
        class={cn("flex h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border px-2.5 text-xs focus-visible:outline-2 focus-visible:outline-primary", personOnly ? "border-primary/60 bg-primary/10 text-foreground" : "border-border text-muted hover:text-foreground")}
        aria-pressed={personOnly}
        aria-label={$t("chat.personOnly")}
        title={$t("chat.personOnlyHint")}
        onclick={() => setPersonOnly(!personOnly)}
      >
        <UserRound size={14} aria-hidden="true" /><span class="hidden lg:inline" aria-hidden="true">{$t("chat.personOnly")}</span>
      </button>
    {/snippet}
  </Toolbar>
  <div class="flex min-h-0 flex-1">
    <div class={cn("relative flex min-w-0 flex-1 flex-col", focus && "hidden md:flex")}>
      {#if mode === "window"}
        <div class="border-b border-border bg-surface-2/60 px-3 py-1.5 text-center text-xs text-muted" role="status">
          {$t("chat.viewingHistory")}
        </div>
      {/if}
      <!-- `relative` keeps absolutely positioned descendants (screen-reader text,
           menus) inside the scroller; otherwise they overflow the page shell and
           the whole app scrolls with the conversation. -->
      <div
        bind:this={viewport}
        onscroll={onScroll}
        {@attach watchReader}
        class="relative flex-1 overflow-y-auto overscroll-contain p-4"
        role="log"
        aria-label={$t("nav.chat")}
        tabindex="-1"
      >
        <!-- Hidden, not unmounted, until the first pin: the height must be real
             for the observer to measure it (collapsing it here would mean the
             observer never fires and the first pin never happens). -->
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
          {#if turns.length === 0 && !showOptimistic && conversationQuery.data}<p class="pt-24 text-center text-sm text-muted">{$t("conversation.empty")}</p>{/if}
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
          <!-- Room for what floats over the bottom, so it never covers the
               newest message. A spacer, not padding: the resize observer
               that keeps the log pinned measures content, not padding. -->
          {#if overlayHeight > 0}<div aria-hidden="true" style:height={`${overlayHeight}px`}></div>{/if}
          <div bind:this={bottomSentinel} aria-hidden="true"></div>
        </div>
      </div>
      <!-- Just above the composer: the way back to the newest message, then
           any off-screen updates. Stacked so neither covers the other. -->
      <div class="pointer-events-none absolute inset-x-0 bottom-2 z-10 flex flex-col items-center gap-2 px-3" bind:clientHeight={overlayHeight}>
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
          <FocusPanel id={focus} cards={cardMap} onclose={closeFocus} onfocus={openFocus} onanswer={answer} onstop={stop} />
        {/key}
      </div>
    {/if}
  </div>
  <Composer
    bind:this={composer}
    {target}
    {targetRunAs}
    onclear={clearTarget}
    onsending={(text) => {
      // Something new said belongs at the live edge, not inside old history.
      if (mode === "window") backToLatest();
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
  {#if dragging}
    <div class="pointer-events-none absolute inset-2 z-30 flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-primary/70 bg-background/80 text-sm text-foreground backdrop-blur-sm">
      <ImagePlus size={28} class="text-primary" aria-hidden="true" />{$t("chat.dropImages")}
    </div>
  {/if}
</div>
