<script lang="ts">
  import { createMutation } from "@tanstack/svelte-query";
  import { ImagePlus, SendHorizontal, X } from "@lucide/svelte";
  import { onDestroy } from "svelte";
  import { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { acceptableSlice, readImage, type AttachedImage } from "../lib/images.js";
  import { pushToast } from "../lib/toast.js";
  import Button from "./ui/Button.svelte";
  import Textarea from "./ui/Textarea.svelte";

  export interface ComposerTarget {
    id: string;
    title: string;
    /** `focus`: talking to the open card; `reply`: answering one of its questions. */
    mode: "focus" | "reply";
    replyTo?: string | undefined;
  }

  type SendVariables = { body: string; images: AttachedImage[]; target: ComposerTarget | null };

  let {
    target,
    onclear,
    onsending,
    onsent,
    onfailed,
  }: {
    target: ComposerTarget | null;
    onclear: () => void;
    onsending: (text: string) => void;
    onsent: (messageId: string, target: ComposerTarget | null) => void;
    onfailed: () => void;
  } = $props();

  let text = $state("");
  let attached = $state<AttachedImage[]>([]);
  let fileRef: HTMLInputElement;
  let input = $state<HTMLTextAreaElement | undefined>();

  export function focusInput(): void {
    input?.focus();
  }

  const send = createMutation<{ ok: boolean; messageId: string }, unknown, SendVariables>(() => ({
    mutationFn: ({ body, images, target: to }) =>
      api.chat(
        body,
        images.map(({ data, mimeType }) => ({ data, mimeType })),
        to ? { target: to.id, focus: to.mode === "focus", ...(to.replyTo ? { replyTo: to.replyTo } : {}) } : {},
      ),
    onSuccess: (result, variables) => onsent(result.messageId, variables.target),
    onError: (error, variables) => {
      onfailed();
      if (text.length === 0) text = variables.body;
      if (attached.length === 0) attached = variables.images;
      pushToast(error instanceof Error ? error.message : String(error));
    },
  }));

  async function attach(files: File[]): Promise<void> {
    const accepted = acceptableSlice(files, attached.length);
    if (accepted.length < files.length) pushToast($t("chat.imageRejected"));
    const read = await Promise.all(accepted.map(readImage));
    attached = [...attached, ...read];
  }

  function dropAttachment(index: number): void {
    const image = attached[index];
    if (image) URL.revokeObjectURL(image.previewUrl);
    attached = attached.filter((_, current) => current !== index);
  }

  function submit(): void {
    const body = text.trim();
    if ((!body && attached.length === 0) || send.isPending) return;
    onsending(body || $t("chat.imageOnly"));
    text = "";
    const images = attached;
    attached = [];
    send.mutate({ body, images, target });
  }

  onDestroy(() => {
    for (const image of attached) URL.revokeObjectURL(image.previewUrl);
  });
</script>

<div class="border-t border-border p-3">
  <div class="mx-auto max-w-3xl">
  {#if target}
    <div class="flex items-center gap-2 pb-2 text-xs">
      <span class="flex min-w-0 items-center gap-1 rounded-full bg-primary/15 px-2 py-0.5 text-primary">
        <span class="truncate">{$t(target.mode === "reply" ? "conversation.replyTarget" : "conversation.target", { title: target.title })}</span>
        <button class="shrink-0 rounded-full hover:text-foreground" aria-label={$t("conversation.clearTarget")} title={$t("conversation.clearTarget")} onclick={onclear}><X size={12} /></button>
      </span>
    </div>
  {/if}
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
    <input bind:this={fileRef} type="file" accept="image/*" multiple class="hidden" onchange={(event) => { const el = event.currentTarget as HTMLInputElement; void attach([...(el.files ?? [])]); el.value = ""; }} />
    <Button variant="outline" size="icon" aria-label={$t("chat.addImage")} onclick={() => fileRef?.click()}><ImagePlus size={16} /></Button>
    <Textarea
      bind:ref={input}
      rows={2}
      bind:value={text}
      placeholder={target ? $t("conversation.placeholderTarget", { title: target.title }) : $t("conversation.placeholder")}
      onpaste={(event) => { const files = [...(event.clipboardData?.files ?? [])]; if (files.length > 0) { event.preventDefault(); void attach(files); } }}
      onkeydown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.isComposing) { event.preventDefault(); submit(); } }}
    />
    <Button onclick={submit} disabled={send.isPending || (text.trim().length === 0 && attached.length === 0)} aria-label={$t("common.send")}><SendHorizontal size={16} /></Button>
  </div>
  </div>
</div>
