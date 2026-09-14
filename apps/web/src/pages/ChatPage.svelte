<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { ImagePlus, SendHorizontal, X } from "@lucide/svelte";
  import { onDestroy } from "svelte";
  import i18n, { t } from "../i18n/index.js";
  import { api, type ApiError, type HidaneEvent } from "../lib/api.js";
  import { acceptableSlice, readImage, type AttachedImage } from "../lib/images.js";
  import { conversationEvents, payloadText } from "../lib/grouping.js";
  import { pendingState } from "../lib/pending.js";
  import { pushToast } from "../lib/toast.js";
  import { cn } from "../lib/utils.js";
  import ChatBubble from "../components/ChatBubble.svelte";
  import Pending from "../components/Pending.svelte";
  import Button from "../components/ui/Button.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  type SendVariables = { body: string; images: AttachedImage[] };

  const queryClient = useQueryClient();
  let text = $state("");
  let optimistic = $state<string | null>(null);
  let attached = $state<AttachedImage[]>([]);
  let endRef: HTMLDivElement;
  let fileRef: HTMLInputElement;

  const eventsQuery = createQuery(() => ({
    queryKey: ["events", "main"],
    queryFn: () => api.events({ thread: "main", tail: 200 }),
  }));
  let all = $derived(eventsQuery.data?.events ?? []);
  let events = $derived(conversationEvents(all));
  let pending = $derived(pendingState(all));
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

  $effect(() => {
    events.length;
    optimistic;
    pending.active;
    queueMicrotask(() => endRef?.scrollIntoView({ behavior: "smooth" }));
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
  <div class="flex-1 space-y-3 overflow-y-auto p-4">
    {#if events.length === 0 && !optimistic}<p class="pt-16 text-center text-sm text-muted">{$t("chat.empty")}</p>{/if}
    {#each events as event (event.id)}<ChatBubble {event} />{/each}
    {#if optimistic}
      {@const optimisticEvent = { id: "optimistic", kind: "user.message", ts: new Date().toISOString(), payload: { text: optimistic } } as unknown as HidaneEvent}
      <ChatBubble event={optimisticEvent} ghost />
    {/if}
    <Pending state={waiting} />
    <div bind:this={endRef}></div>
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
