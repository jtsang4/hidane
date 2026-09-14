<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Archive, SendHorizontal, Square } from "@lucide/svelte";
  import { SvelteMap } from "svelte/reactivity";
  import { t } from "../i18n/index.js";
  import { api, ApiError, type HidaneEvent, type WorkItemStatus } from "../lib/api.js";
  import { conversationEvents, executionGroups, payloadText } from "../lib/grouping.js";
  import { pendingState, withActiveWorker } from "../lib/pending.js";
  import { liveRepliesFor, maxSeq } from "../lib/liveText.js";
  import { nextCursor } from "../lib/pagination.js";
  import { pushToast } from "../lib/toast.js";
  import { cn, fmtDateTime } from "../lib/utils.js";
  import Artifacts from "../components/Artifacts.svelte";
  import Execution from "../components/Execution.svelte";
  import ChatBubble from "../components/ChatBubble.svelte";
  import Pending from "../components/Pending.svelte";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  let { id }: { id: string } = $props();

  /** A busy item emits executions and side effects far faster than messages,
   *  so the thread is paged like the chat rather than fetched whole. */
  const PAGE_SIZE = 100;

  const queryClient = useQueryClient();
  let text = $state("");
  let optimistic = $state<string | null>(null);
  let olderPages = $state<HidaneEvent[][]>([]);
  let loadingOlder = $state(false);
  let exhausted = $state(false);
  const seen = new SvelteMap<string, HidaneEvent>();

  const itemQuery = createQuery(() => ({
    queryKey: ["item", id],
    queryFn: () => api.workItem(id, PAGE_SIZE),
    retry: false,
  }));
  let data = $derived(itemQuery.data);

  $effect(() => {
    const page = itemQuery.data?.events;
    if (!page) return;
    for (const event of page) seen.set(event.id, event);
  });

  // Same append-only union the chat uses: the newest page slides forward on
  // every live event, so concatenating it onto frozen older pages loses
  // whatever landed in between.
  let events = $derived([...seen.values()].sort((a, b) => a.seq - b.seq));
  let conversation = $derived(conversationEvents(events));
  let executions = $derived(executionGroups(events));
  let hasOlder = $derived((data?.hasMore ?? false) && !exhausted);
  let pending = $derived(withActiveWorker(pendingState(events), data?.running ?? false));
  /** Manager replies still being written in this item's thread. */
  let live = $derived(liveRepliesFor(data?.item.threadId ?? "", maxSeq(conversation)));
  let waiting = $derived(
    pending.active
      ? pending
      : optimistic !== null
        ? { active: true, since: new Date().toISOString(), phase: "routing" as const }
        : pending,
  );

  async function loadOlder(): Promise<void> {
    const thread = data?.item.threadId;
    if (!thread || loadingOlder || exhausted) return;
    const cursor = nextCursor(itemQuery.data?.events ?? [], olderPages);
    if (cursor === undefined) return;
    loadingOlder = true;
    try {
      const page = await api.eventsPage({ thread, before: cursor, limit: PAGE_SIZE });
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

  $effect(() => {
    if (optimistic && conversation.some((event) => event.kind === "user.message" && payloadText(event) === optimistic)) optimistic = null;
  });

  const send = createMutation<{ ok: boolean }, unknown, string>(() => ({
    mutationFn: (body) => api.threadMessage(id, body),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["item", id] }),
    onError: (error, body) => {
      optimistic = null;
      if (text.length === 0) text = body;
      pushToast(error instanceof Error ? error.message : String(error));
    },
  }));
  const cancel = createMutation<{ ok: boolean; executionId: string | null }, unknown, void>(() => ({
    mutationFn: () => api.cancelExecution(id),
    onSuccess: () => {
      pushToast($t("item.cancelled"), "default");
      void queryClient.invalidateQueries({ queryKey: ["item", id] });
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));
  const setStatus = createMutation<{ ok: boolean; item: unknown }, unknown, WorkItemStatus>(() => ({
    mutationFn: (status) => api.setWorkItemStatus(id, status),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["item", id] });
      void queryClient.invalidateQueries({ queryKey: ["items"] });
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));

  function submit(): void {
    const body = text.trim();
    if (!body || send.isPending) return;
    optimistic = body;
    text = "";
    send.mutate(body);
  }
</script>

{#if itemQuery.error}
  <p class="p-6 text-sm text-muted">{$t("item.notFound")}</p>
{:else if !data}
  <p class="p-6 text-sm text-muted">{$t("common.loading")}</p>
{:else}
  {@const item = data.item}
  <div class="flex h-full flex-col">
    <div class="border-b border-border p-4">
      <div class="flex flex-wrap items-center gap-2">
        <h1 class="text-lg font-semibold">{item.title}</h1>
        <Badge tone={item.status === "open" ? "success" : "muted"}>{item.status}</Badge>
        <div class="ml-auto flex items-center gap-2">
          {#if pending.phase === "executing"}
            <Button variant="outline" size="sm" disabled={cancel.isPending} onclick={() => { if (confirm($t("item.confirmCancel"))) cancel.mutate(); }}><Square size={12} />{$t("item.cancel")}</Button>
          {/if}
          <Button variant="outline" size="sm" disabled={setStatus.isPending} onclick={() => setStatus.mutate(item.status === "open" ? "done" : "open")}>{item.status === "open" ? $t("item.markDone") : $t("item.reopen")}</Button>
          {#if item.status !== "closed"}
            <Button variant="ghost" size="icon" aria-label={$t("item.archive")} title={$t("item.archive")} disabled={setStatus.isPending} onclick={() => { if (confirm($t("item.confirmArchive"))) setStatus.mutate("closed"); }}><Archive size={16} /></Button>
          {/if}
        </div>
      </div>
      <p class="mt-1 text-xs text-muted">{$t("item.meta", { id: item.id, time: fmtDateTime(item.createdAt), workspace: item.workspace })}</p>
    </div>
    <div class="flex-1 space-y-4 overflow-y-auto p-4">
      <section class="space-y-2">
        {#if hasOlder}
          <div class="text-center">
            <Button variant="outline" size="sm" onclick={() => void loadOlder()} disabled={loadingOlder}>
              {loadingOlder ? $t("common.loading") : $t("chat.loadEarlier")}
            </Button>
          </div>
        {/if}
        {#each conversation as event (event.id)}<ChatBubble {event} />{/each}
        {#if optimistic}
          <div class="flex justify-end"><div class="max-w-[85%] rounded-lg bg-primary px-3 py-2 text-sm whitespace-pre-wrap break-words text-primary-foreground opacity-60">{optimistic}<div class="mt-1 text-[10px] opacity-60">{$t("pending.sending")}</div></div></div>
        {/if}
        {#each live as reply (reply.id)}
          {@const liveEvent = { seq: 0, id: reply.id, ts: reply.ts, source: "agent:manager", kind: "agent.reply", threadId: reply.threadId, workItemId: id, executionId: null, payload: { text: reply.text } } satisfies HidaneEvent}
          <ChatBubble event={liveEvent} streaming={!reply.done} />
        {/each}
        <!-- The spinner and the reply are the same wait; showing both reads as
             two things happening at once. -->
        {#if live.length === 0}<Pending state={waiting} />{/if}
      </section>
      <Artifacts workItemId={id} />
      {#if executions.length > 0}
        <section class="space-y-2">
          <h2 class="text-sm font-medium text-muted">{$t("item.timeline")}</h2>
          {#each executions as group (group.executionId)}<Execution {group} />{/each}
        </section>
      {/if}
    </div>
    <div class="border-t border-border p-3">
      {#if pending.phase === "executing"}<p class="pb-2 text-xs text-muted">{$t("item.steerHint")}</p>{/if}
      <div class="flex gap-2">
        <Textarea rows={2} bind:value={text} placeholder={$t("item.replyPlaceholder")} onkeydown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); submit(); } }} />
        <Button onclick={submit} disabled={send.isPending || text.trim().length === 0} aria-label={$t("common.send")}><SendHorizontal size={16} /></Button>
      </div>
    </div>
  </div>
{/if}
