<script lang="ts">
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import ImagePlus from "@lucide/svelte/icons/image-plus";
  import ArrowUp from "@lucide/svelte/icons/arrow-up";
  import X from "@lucide/svelte/icons/x";
  import { onDestroy } from "svelte";
  import { MediaQuery } from "svelte/reactivity";
  import { t } from "../i18n/index.js";
  import i18n from "../i18n/index.js";
  import { api, type Effort, type RunAs } from "../lib/api.js";
  import { acceptableSlice, readImage, type AttachedImage } from "../lib/images.js";
  import { loadDraftRunAs, runAsSummary, saveDraftRunAs } from "../lib/runAs.js";
  import { pushToast, toastError } from "../lib/toast.js";
  import RunAsBar from "./RunAsBar.svelte";
  import Button from "./ui/Button.svelte";
  import Textarea from "./ui/Textarea.svelte";

  export interface ComposerTarget {
    id: string;
    title: string;
    /** `focus`: talking to the open card; `reply`: answering one of its questions. */
    mode: "focus" | "reply";
    replyTo?: string | undefined;
  }

  type SendVariables = { body: string; images: AttachedImage[]; target: ComposerTarget | null; runAs: RunAs | null };

  let {
    target,
    targetRunAs = null,
    onclear,
    onsending,
    onsent,
    onfailed,
  }: {
    target: ComposerTarget | null;
    /** What the addressed task runs on (`null`: the settings). */
    targetRunAs?: RunAs | null;
    onclear: () => void;
    onsending: (text: string) => void;
    onsent: (messageId: string, target: ComposerTarget | null) => void;
    onfailed: () => void;
  } = $props();

  const queryClient = useQueryClient();
  /** What new tasks from this composer run on; remembered on this machine. */
  let draftRunAs = $state<RunAs | null>(loadDraftRunAs());
  let text = $state("");
  let attached = $state<AttachedImage[]>([]);
  let fileRef: HTMLInputElement;
  let input = $state<HTMLTextAreaElement | undefined>();

  export function focusInput(): void {
    input?.focus();
  }

  /** Images dropped on the conversation land here, as if picked. */
  export function attachFiles(files: File[]): void {
    void attach(files);
  }

  const send = createMutation<{ ok: boolean; messageId: string }, unknown, SendVariables>(() => ({
    mutationFn: ({ body, images, target: to, runAs }) =>
      api.chat(
        body,
        images.map(({ data, mimeType }) => ({ data, mimeType })),
        to ? { target: to.id, focus: to.mode === "focus", ...(to.replyTo ? { replyTo: to.replyTo } : {}) } : runAs ? { runAs } : {},
      ),
    onSuccess: (result, variables) => onsent(result.messageId, variables.target),
    onError: (error, variables) => {
      onfailed();
      if (text.length === 0) text = variables.body;
      if (attached.length === 0) attached = variables.images;
      toastError(error);
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
    sparks += 1;
    text = "";
    const images = attached;
    attached = [];
    send.mutate({ body, images, target, runAs: draftRunAs });
  }

  /**
   * Choices are saved one after another, in the order they were made, so a
   * slow save can never land after — and undo — a later one.
   */
  let saves: Promise<void> = Promise.resolve();
  const latestSave = { conversation: 0, task: 0 };

  /**
   * Settles once the change is saved and the queries the pickers read show
   * it — or, when it failed, once they show what the server has instead.
   */
  function queueSave(kind: keyof typeof latestSave, save: (latest: () => boolean) => Promise<void>, rollback: () => Promise<unknown>): Promise<void> {
    const seq = ++latestSave[kind];
    const done = saves.then(() =>
      save(() => seq === latestSave[kind]).catch(async (error: unknown) => {
        toastError(error);
        await rollback();
      }),
    );
    saves = done;
    return done;
  }

  const effortLabel = (effort: Effort): string => i18n.t(`settings.effort.${effort || "default"}`);
  const summary = (value: RunAs): string => runAsSummary(value, effortLabel, i18n.t("runAs.defaultModel"));

  /** Who answers in the conversation is the Primary role's setting: saved at once, from the next message on. */
  function chooseConversation(next: RunAs): Promise<void> {
    return queueSave(
      "conversation",
      async (latest) => {
        const saved = await api.saveRoles({ primary: next });
        if (!latest()) return;
        queryClient.setQueryData(["settings"], saved.settings);
        void queryClient.invalidateQueries({ queryKey: ["status"] });
        pushToast(i18n.t("runAs.conversationChanged", { summary: summary(next) }), "default");
      },
      () => queryClient.invalidateQueries({ queryKey: ["settings"] }),
    );
  }

  /** A new task's choice is remembered; an addressed task's is changed on the task, now. */
  function chooseRunAs(next: RunAs | null): Promise<void> | undefined {
    if (!target) {
      draftRunAs = next;
      saveDraftRunAs(next);
      return undefined;
    }
    // No comparison with `targetRunAs`: it may still be the value before a save in flight.
    const { id, title } = target;
    const refresh = (): Promise<unknown> =>
      Promise.all([queryClient.invalidateQueries({ queryKey: ["board"] }), queryClient.invalidateQueries({ queryKey: ["items"] })]);
    return queueSave("task", async (latest) => {
      await api.setWorkItemRunAs(id, next);
      if (!latest()) return;
      await refresh();
      pushToast(next ? i18n.t("runAs.changed", { title, summary: summary(next) }) : i18n.t("runAs.released", { title }), "default");
    }, refresh);
  }

  /** Counts sends; each one replays the send button's spark. */
  let sparks = $state(0);

  // A phone has no Enter-to-send or paste to explain, and the long hint wrapped into an orphan line.
  const narrow = new MediaQuery("max-width: 639px");

  onDestroy(() => {
    for (const image of attached) URL.revokeObjectURL(image.previewUrl);
  });
</script>

<div class="border-t border-border px-4 pt-3 pb-3">
  <div class="mx-auto max-w-3xl">
  {#if target}
    <div class="flex items-center gap-2 pb-2 text-xs">
      <span class="flex h-6 min-w-0 items-center gap-1 rounded-md bg-primary/12 pr-1 pl-2 text-primary">
        <span class="truncate">{$t(target.mode === "reply" ? "conversation.replyTarget" : "conversation.target", { title: target.title })}</span>
        <button class="flex size-4 shrink-0 items-center justify-center rounded-sm hover:bg-primary/20 coarse:size-7" aria-label={$t("conversation.clearTarget")} title={$t("conversation.clearTarget")} onclick={onclear}><X size={12} /></button>
      </span>
    </div>
  {/if}
  {#if attached.length > 0}
    <div class="flex flex-wrap gap-2 pb-2">
      {#each attached as image, index (image.previewUrl)}
        <div class="relative">
          <img src={image.previewUrl} alt={image.name} class="size-14 rounded-md border border-border object-cover" />
          <button class="absolute -top-1.5 -right-1.5 rounded-full border border-border bg-popover p-0.5 text-muted hover:text-foreground" aria-label={`${$t("chat.removeImage")} ${image.name}`} onclick={() => dropAttachment(index)}>
            <X size={11} />
          </button>
        </div>
      {/each}
    </div>
  {/if}
  <!-- One field: the text on top, what it is sent with along its bottom edge. -->
  <div class="rounded-lg border border-input bg-field transition-colors hover:border-input-strong focus-within:border-primary/50 focus-within:ring-3 focus-within:ring-primary/10">
    <input bind:this={fileRef} type="file" accept="image/*" multiple class="hidden" onchange={(event) => { const el = event.currentTarget as HTMLInputElement; void attach([...(el.files ?? [])]); el.value = ""; }} />
    <Textarea
      bind:ref={input}
      rows={2}
      bind:value={text}
      class="block border-0 bg-transparent px-3 pt-2.5 pb-1 hover:border-0 focus-visible:ring-0"
      placeholder={target ? $t("conversation.placeholderTarget", { title: target.title }) : $t(narrow.current ? "conversation.placeholderShort" : "conversation.placeholder")}
      onpaste={(event) => { const files = [...(event.clipboardData?.files ?? [])]; if (files.length > 0) { event.preventDefault(); void attach(files); } }}
      onkeydown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.isComposing) { event.preventDefault(); submit(); } }}
    />
    <div class="flex items-center gap-1 px-1.5 pb-1.5">
      <Button variant="ghost" size="icon" aria-label={$t("chat.addImage")} onclick={() => fileRef?.click()}><ImagePlus /></Button>
      <div class="min-w-0 flex-1">
        <!-- A choice still being saved belongs to the task it was made for, not the next one addressed. -->
        {#key target?.id}
          <RunAsBar
            value={target ? targetRunAs : draftRunAs}
            scope={target ? "task" : "new"}
            onchange={chooseRunAs}
            onconversation={target ? undefined : chooseConversation}
          />
        {/key}
      </div>
      <span class="relative flex">
      {#key sparks}{#if sparks > 0}<span class="pointer-events-none absolute inset-0 animate-spark rounded-md border-[1.5px] border-primary/70 shadow-ember-spark" aria-hidden="true"></span>{/if}{/key}
      <Button size="icon" onclick={submit} disabled={send.isPending || (text.trim().length === 0 && attached.length === 0)} aria-label={$t("common.send")}><ArrowUp /></Button>
      </span>
    </div>
  </div>
  </div>
</div>
