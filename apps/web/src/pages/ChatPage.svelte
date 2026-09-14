<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { ImagePlus, SendHorizontal, X } from "@lucide/svelte";
  import { onDestroy, tick } from "svelte";
  import { SvelteMap } from "svelte/reactivity";
  import i18n, { t } from "../i18n/index.js";
  import { api, type ApiError, type HidaneEvent } from "../lib/api.js";
  import { acceptableSlice, readImage, type AttachedImage } from "../lib/images.js";
  import { conversationEvents, CONVERSATION_KINDS, payloadText } from "../lib/grouping.js";
  import { pendingState } from "../lib/pending.js";
  import { liveRepliesFor } from "../lib/liveText.js";
  import { pushToast } from "../lib/toast.js";
  import { isPinnedToBottom } from "../lib/scroll.js";
  import { nextCursor } from "../lib/pagination.js";
  import { matchesQuery } from "../lib/search.js";
  import { cn } from "../lib/utils.js";
  import ChatBubble from "../components/ChatBubble.svelte";
  import Pending from "../components/Pending.svelte";
  import Button from "../components/ui/Button.svelte";
  import Input from "../components/ui/Input.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  type SendVariables = { body: string; images: AttachedImage[] };

  /** Bubbles per request. Older pages are pulled in as the reader scrolls up.
   *  The server filters by kind, so a page is this many bubbles exactly. */
  const PAGE_SIZE = 40;
  const KINDS = CONVERSATION_KINDS.join(",");

  const queryClient = useQueryClient();
  let text = $state("");
  let optimistic = $state<string | null>(null);
  let attached = $state<AttachedImage[]>([]);
  let viewport = $state<HTMLDivElement | undefined>();
  let content = $state<HTMLDivElement | undefined>();
  let topSentinel = $state<HTMLDivElement | undefined>();
  let fileRef: HTMLInputElement;
  /** Cursor bookkeeping only — rendering reads the union in `seen`. */
  let olderPages = $state<HidaneEvent[][]>([]);
  let loadingOlder = $state(false);
  let exhausted = $state(false);
  let query = $state("");
  /**
   * Whether the reader is sitting at the live edge and wants to stay there.
   * Cleared when they scroll up into history, restored when they come back.
   */
  let follow = $state(true);
  /**
   * Whether the first page has been pinned to the live edge yet.
   *
   * Paging must not arm before that. On an unscrolled viewport the top sentinel
   * is trivially in view, so a second page loads before the reader has seen the
   * first — and that load's scroll anchor, captured above the live edge, then
   * overrides the pin and opens the chat partway up the history.
   */
  let primed = $state(false);

  /**
   * Every event this session has observed, keyed by id.
   *
   * The newest page is refetched on each live event and so slides forward,
   * while an older page keeps the exclusive `before` it was fetched with.
   * Concatenating the two drops whatever was appended in between. Retaining
   * instead is sound because the log is append-only.
   */
  const seen = new SvelteMap<string, HidaneEvent>();

  const eventsQuery = createQuery(() => ({
    queryKey: ["events", "main"],
    queryFn: () => api.eventsPage({ thread: "main", kind: KINDS, limit: PAGE_SIZE }),
  }));

  $effect(() => {
    const page = eventsQuery.data?.events;
    if (!page) return;
    for (const event of page) seen.set(event.id, event);
  });

  let all = $derived([...seen.values()].sort((a, b) => a.seq - b.seq));
  // Defensive: a daemon that ignores the `kind` param would otherwise render
  // routing decisions as chat bubbles.
  let events = $derived(conversationEvents(all));
  let pending = $derived(pendingState(all));
  let searching = $derived(query.trim().length > 0);
  /** Replies still being written. Cleared by the durable event that records them. */
  let live = $derived(liveRepliesFor("main"));
  let visible = $derived(searching ? events.filter((event) => matchesQuery(event, query)) : events);
  let hasOlder = $derived((eventsQuery.data?.hasMore ?? false) && !exhausted);
  let waiting = $derived(
    pending.active
      ? pending
      : optimistic !== null
        ? { active: true, since: new Date().toISOString(), phase: "routing" as const }
        : pending,
  );

  const send = createMutation<{ ok: boolean }, unknown, SendVariables>(() => ({
    mutationFn: ({ body, images }) =>
      api.chat(
        body,
        images.map(({ data, mimeType }) => ({ data, mimeType })),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["events", "main"] });
    },
    onError: (error, variables) => {
      optimistic = null;
      if (text.length === 0) text = variables.body;
      if (attached.length === 0) attached = variables.images;
      pushToast(error instanceof Error ? error.message : String(error));
    },
  }));

  $effect(() => {
    if (optimistic && events.some((event) => event.kind === "user.message" && payloadText(event) === optimistic)) {
      optimistic = null;
    }
  });

  /**
   * Follow the live edge by watching rendered height, not the event list.
   *
   * Scrolling in response to the data change cannot work: the content is not in
   * the DOM yet when the effect runs, and it keeps settling afterwards as images
   * decode and fonts swap, so the jump lands short of the bottom. A layout
   * observer fires once the height is real, however it got there.
   *
   * The jump is deliberately instant. Smooth scrolling animated the whole loaded
   * history past the reader on first paint, which read as the page loading
   * message-by-message from the very first one.
   */
  $effect(() => {
    const el = viewport;
    const inner = content;
    if (!el || !inner) return;
    const observer = new ResizeObserver(() => {
      if (events.length === 0) return;
      if (follow && !searching) el.scrollTo(0, el.scrollHeight);
      // Set last but in the same callback, so the frame that reveals the list is
      // already scrolled. Set unconditionally: tying it to the pin would leave
      // the list hidden for good if the reader were somehow not following.
      primed = true;
    });
    observer.observe(inner);
    return () => observer.disconnect();
  });

  function trackFollow(): void {
    const el = viewport;
    if (el) follow = isPinnedToBottom(el);
  }

  async function loadOlder(): Promise<void> {
    if (loadingOlder || exhausted) return;
    const cursor = nextCursor(eventsQuery.data?.events ?? [], olderPages);
    if (cursor === undefined) return;
    const el = viewport;
    // Distance from the bottom survives a prepend; scrollTop does not. Restoring
    // it is what keeps the reader on the message they were looking at.
    const fromBottom = el ? el.scrollHeight - el.scrollTop : 0;
    loadingOlder = true;
    try {
      const page = await api.eventsPage({
        thread: "main",
        kind: KINDS,
        before: cursor,
        limit: PAGE_SIZE,
      });
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
   * Scroll-up paging. The overflow guard matters: in a conversation too short
   * to scroll, the sentinel sits on screen permanently and would otherwise walk
   * the entire history in one burst.
   *
   * Re-armed per page so that a viewport still not filled after a load triggers
   * the next one — an observer whose sentinel never leaves view fires only once.
   *
   * Held off while searching: a short result list leaves the sentinel parked on
   * screen, which would walk the whole history in the background and silently
   * widen a search the reader scoped to what was loaded. The button stays.
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

  async function attach(files: File[]): Promise<void> {
    const accepted = acceptableSlice(files, attached.length);
    if (accepted.length < files.length) pushToast($t("chat.imageRejected"));
    const read = await Promise.all(accepted.map(readImage));
    attached = [...attached, ...read];
  }

  function dropAttachment(index: number): void {
    const target = attached[index];
    if (target) URL.revokeObjectURL(target.previewUrl);
    attached = attached.filter((_, current) => current !== index);
  }

  function submit(): void {
    const body = text.trim();
    if ((!body && attached.length === 0) || send.isPending) return;
    optimistic = body || $t("chat.imageOnly");
    text = "";
    const images = attached;
    attached = [];
    send.mutate({ body, images });
  }

  onDestroy(() => {
    for (const image of attached) URL.revokeObjectURL(image.previewUrl);
  });
</script>

<div class="flex h-full flex-col">
  <div class="border-b border-border p-2">
    <Input bind:value={query} placeholder={$t("chat.search")} aria-label={$t("chat.search")} />
    {#if searching}
      <p class="px-1 pt-1 text-xs text-muted">
        {$t("chat.searchMatches", { n: visible.length })}{#if hasOlder}<span class="px-1">·</span>{$t("chat.searchPartial")}{/if}
      </p>
    {/if}
  </div>
  <div bind:this={viewport} onscroll={trackFollow} class="flex-1 overflow-y-auto p-4">
    <!-- Hidden, not unmounted, until the first pin: the height must be real for
         the observer to measure it, but showing the top of the history for the
         frames before the jump is the flash this page is meant to avoid. -->
    <div bind:this={content} class={cn("space-y-3", events.length > 0 && !primed && "invisible")}>
      <div bind:this={topSentinel} aria-hidden="true"></div>
      {#if hasOlder}
        <div class="text-center">
          <Button variant="outline" size="sm" onclick={() => void loadOlder()} disabled={loadingOlder}>
            {loadingOlder ? $t("common.loading") : $t("chat.loadEarlier")}
          </Button>
        </div>
      {:else if events.length > 0}
        <p class="text-center text-xs text-muted">{$t("chat.historyStart")}</p>
      {/if}
      {#if events.length === 0 && !optimistic}<p class="pt-16 text-center text-sm text-muted">{$t("chat.empty")}</p>{/if}
      {#if searching && visible.length === 0 && events.length > 0}<p class="pt-8 text-center text-sm text-muted">{$t("chat.searchEmpty")}</p>{/if}
      {#each visible as event (event.id)}<ChatBubble {event} />{/each}
      {#if !searching}
        {#if optimistic}
          {@const optimisticEvent = { id: "optimistic", kind: "user.message", ts: new Date().toISOString(), payload: { text: optimistic } } as unknown as HidaneEvent}
          <ChatBubble event={optimisticEvent} ghost />
        {/if}
        {#each live as reply (reply.id)}
          {@const liveEvent = { seq: 0, id: reply.id, ts: reply.ts, source: "agent:primary", kind: "agent.reply", threadId: reply.threadId, workItemId: null, executionId: null, payload: { text: reply.text } } satisfies HidaneEvent}
          <ChatBubble event={liveEvent} streaming={!reply.done} />
        {/each}
        <!-- The spinner and the reply are the same wait; showing both reads as
             two things happening at once. -->
        {#if live.length === 0}<Pending state={waiting} />{/if}
      {/if}
    </div>
  </div>
  <div class="border-t border-border p-3">
    {#if attached.length > 0}
      <div class="flex flex-wrap gap-2 pb-2">
        {#each attached as image, index (image.previewUrl)}
          <div class="relative">
            <img src={image.previewUrl} alt={image.name} class="h-16 w-16 rounded-md border border-border object-cover" />
            <button class="absolute -top-1.5 -right-1.5 rounded-full bg-surface-2 p-0.5 text-muted hover:text-foreground" aria-label={`${$t("chat.removeImage")} ${image.name}`} onclick={() => dropAttachment(index)}>
              <X size={12} />
            </button>
          </div>
        {/each}
      </div>
    {/if}
    <div class="flex gap-2">
      <input bind:this={fileRef} type="file" accept="image/*" multiple class="hidden" onchange={(event) => { const input = event.currentTarget as HTMLInputElement; void attach([...(input.files ?? [])]); input.value = ""; }} />
      <Button variant="outline" size="icon" aria-label={$t("chat.addImage")} onclick={() => fileRef?.click()}><ImagePlus size={16} /></Button>
      <Textarea
        rows={2}
        bind:value={text}
        placeholder={$t("chat.placeholder")}
        onpaste={(event) => { const files = [...(event.clipboardData?.files ?? [])]; if (files.length > 0) { event.preventDefault(); void attach(files); } }}
        onkeydown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); submit(); } }}
      />
      <Button onclick={submit} disabled={send.isPending || (text.trim().length === 0 && attached.length === 0)} aria-label={$t("common.send")}><SendHorizontal size={16} /></Button>
    </div>
  </div>
</div>
