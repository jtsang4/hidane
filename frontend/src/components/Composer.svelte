<script lang="ts">
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import ImagePlus from "@lucide/svelte/icons/image-plus";
  import ArrowUp from "@lucide/svelte/icons/arrow-up";
  import AtSign from "@lucide/svelte/icons/at-sign";
  import SquareTerminal from "@lucide/svelte/icons/square-terminal";
  import X from "@lucide/svelte/icons/x";
  import { onDestroy, tick } from "svelte";
  import { MediaQuery } from "svelte/reactivity";
  import { t } from "../i18n/index.js";
  import i18n from "../i18n/index.js";
  import { api, type Effort, type RunAs } from "../lib/api.js";
  import { acceptableSlice, readImage, type AttachedImage } from "../lib/images.js";
  import { cutRange, matchCommands, mentionAt, parseSlash, rankCandidates, slashAt, type Candidate, type MentionQuery, type SlashCommand } from "../lib/mentions.js";
  import { loadDraftRunAs, runAsSummary, saveDraftRunAs } from "../lib/runAs.js";
  import { popover, popoverItem, popoverLabel } from "../lib/styles.js";
  import { runSlashCommand } from "../lib/taskActions.js";
  import { pushToast, toastError } from "../lib/toast.js";
  import { cn } from "../lib/utils.js";
  import RunAsBar from "./RunAsBar.svelte";
  import StateDot from "./StateDot.svelte";
  import Button from "./ui/Button.svelte";
  import Textarea from "./ui/Textarea.svelte";

  export interface ComposerTarget {
    id: string;
    title: string;
    /** `focus`: talking to the open card; `reply`: answering one of its questions. */
    mode: "focus" | "reply";
    replyTo?: string | undefined;
  }

  type Addressee = { id: string; title: string };
  type SendVariables = { body: string; images: AttachedImage[]; lead: ComposerTarget | null; mentioned: Addressee[]; runAs: RunAs | null };

  let {
    target,
    candidates = [],
    runAsOf = () => null,
    onclear,
    onsending,
    onsent,
    onfailed,
  }: {
    /** Who the page addresses: the focused card, or a question being answered. */
    target: ComposerTarget | null;
    /** Tasks an `@` can name. */
    candidates?: readonly Candidate[];
    /** What an addressed task runs on (`null`: the settings). */
    runAsOf?: (id: string) => RunAs | null;
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
  /** Tasks named with `@` for the next message; they go once it is sent. */
  let mentioned = $state<Addressee[]>([]);
  /** The `@` or `/` being typed, and the row the keyboard is on. */
  let picking = $state<(MentionQuery & { kind: "mention" | "slash" }) | null>(null);
  let highlight = $state(0);
  let running = $state(false);
  let fileRef: HTMLInputElement;
  let input = $state<HTMLTextAreaElement | undefined>();

  /** Everyone the next message goes to, the page's own target first. */
  let addressees = $derived.by((): Addressee[] => {
    const out: Addressee[] = target ? [{ id: target.id, title: target.title }] : [];
    for (const m of mentioned) if (!out.some((a) => a.id === m.id)) out.push(m);
    return out;
  });
  let only = $derived(addressees.length === 1 ? addressees[0] : undefined);
  /** What the picker offers: tasks for an `@`, commands for a `/`. */
  let choices = $derived.by((): (Candidate | SlashCommand)[] => {
    if (picking?.kind === "mention") return rankCandidates(candidates, picking.query, new Set(addressees.map((a) => a.id)));
    return picking?.kind === "slash" ? matchCommands(picking.query) : [];
  });
  const uid = $props.id();
  const listId = `${uid}-picker`;

  export function focusInput(): void {
    input?.focus();
  }

  /** Images dropped on the conversation land here, as if picked. */
  export function attachFiles(files: File[]): void {
    void attach(files);
  }

  const send = createMutation<{ ok: boolean; messageId: string }, unknown, SendVariables>(() => ({
    mutationFn: ({ body, images, lead, mentioned: named, runAs }) => {
      const others = named.map((m) => m.id).filter((id) => id !== lead?.id);
      const addressed = lead || others.length > 0;
      return api.chat(
        body,
        images.map(({ data, mimeType }) => ({ data, mimeType })),
        {
          ...(lead ? { target: lead.id, focus: lead.mode === "focus", ...(lead.replyTo ? { replyTo: lead.replyTo } : {}) } : {}),
          ...(others.length > 0 ? { targets: others } : {}),
          ...(!addressed && runAs ? { runAs } : {}),
        },
      );
    },
    onSuccess: (result, variables) => onsent(result.messageId, variables.lead),
    onError: (error, variables) => {
      onfailed();
      if (text.length === 0) text = variables.body;
      if (attached.length === 0) attached = variables.images;
      if (mentioned.length === 0) mentioned = variables.mentioned;
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

  /** A command acts on the addressed tasks at once; it is not a message. */
  async function runCommand(command: SlashCommand): Promise<void> {
    if (addressees.length === 0) {
      pushToast($t("slash.needTarget"));
      return;
    }
    running = true;
    try {
      const done = await runSlashCommand(queryClient, command, addressees);
      if (!done) return;
      text = "";
      mentioned = [];
      if (target?.mode === "reply") onclear();
    } finally {
      running = false;
    }
  }

  function submit(): void {
    const body = text.trim();
    if ((!body && attached.length === 0) || send.isPending || running) return;
    const command = attached.length === 0 ? parseSlash(body) : null;
    if (command) {
      void runCommand(command);
      return;
    }
    onsending(body || $t("chat.imageOnly"));
    sparks += 1;
    text = "";
    const images = attached;
    attached = [];
    const named = mentioned;
    mentioned = [];
    picking = null;
    send.mutate({ body, images, lead: target, mentioned: named, runAs: draftRunAs });
  }

  /** What the caret is in: an `@` word, or a `/` command typed as the whole message. */
  function syncPicker(): void {
    const el = input;
    if (!el) return;
    const caret = el.selectionStart ?? text.length;
    const mention = mentionAt(text, caret);
    const slash = mention ? null : slashAt(text, caret);
    const next = mention ? { ...mention, kind: "mention" as const } : slash ? { ...slash, kind: "slash" as const } : null;
    if (next?.kind !== picking?.kind || next?.query !== picking?.query) highlight = 0;
    picking = next;
  }

  async function placeCaret(at: number): Promise<void> {
    await tick();
    input?.focus();
    input?.setSelectionRange(at, at);
  }

  function chooseMention(choice: Candidate): void {
    if (!picking) return;
    const cut = cutRange(text, picking.start, picking.end);
    text = cut.text;
    mentioned = [...mentioned, { id: choice.id, title: choice.title }];
    picking = null;
    void placeCaret(cut.caret);
  }

  function chooseCommand(command: SlashCommand): void {
    text = `/${command} `;
    picking = null;
    void placeCaret(text.length);
  }

  function choose(choice: Candidate | SlashCommand): void {
    if (typeof choice === "string") chooseCommand(choice);
    else chooseMention(choice);
  }

  /** The @ button: start an `@` where the caret is, as if typed. */
  function startMention(): void {
    const el = input;
    const caret = el?.selectionStart ?? text.length;
    const before = text.slice(0, caret);
    const insert = before === "" || /\s$/.test(before) ? "@" : " @";
    text = before + insert + text.slice(caret);
    void placeCaret(caret + insert.length).then(syncPicker);
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.isComposing) return;
    if (picking && choices.length > 0) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        highlight = (highlight + (event.key === "ArrowDown" ? 1 : choices.length - 1)) % choices.length;
        return;
      }
      if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        const choice = choices[highlight];
        if (choice) choose(choice);
        return;
      }
    }
    if (picking && event.key === "Escape") {
      // The picker closes; nothing behind it (a focused card, settings) should.
      event.preventDefault();
      event.stopPropagation();
      picking = null;
      return;
    }
    if (event.key === "Backspace" && text === "" && mentioned.length > 0) {
      mentioned = mentioned.slice(0, -1);
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submit();
    }
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
    if (!only) {
      draftRunAs = next;
      saveDraftRunAs(next);
      return undefined;
    }
    // No comparison with what the task runs on now: it may still be the value before a save in flight.
    const { id, title } = only;
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

  let placeholder = $derived(
    addressees.length > 1
      ? $t("conversation.placeholderTargets", { count: addressees.length })
      : only
        ? $t("conversation.placeholderTarget", { title: only.title })
        : $t(narrow.current ? "conversation.placeholderShort" : "conversation.placeholder"),
  );

  const chip = "flex h-6 min-w-0 max-w-full items-center gap-1 rounded-md bg-primary/12 pr-1 pl-2 text-primary";
  const chipClear = "flex size-4 shrink-0 items-center justify-center rounded-sm hover:bg-primary/20 coarse:size-7";

  onDestroy(() => {
    for (const image of attached) URL.revokeObjectURL(image.previewUrl);
  });
</script>

<div class="border-t border-border px-4 pt-3 pb-3">
  <div class="mx-auto max-w-3xl">
  {#if addressees.length > 0}
    <div class="flex flex-wrap items-center gap-1.5 pb-2 text-xs">
      {#if target}
        <span class={chip}>
          <span class="truncate">{$t(target.mode === "reply" ? "conversation.replyTarget" : "conversation.target", { title: target.title })}</span>
          <button class={chipClear} aria-label={$t("conversation.clearTarget")} title={$t("conversation.clearTarget")} onclick={onclear}><X size={12} /></button>
        </span>
      {/if}
      {#each mentioned.filter((m) => m.id !== target?.id) as m (m.id)}
        <span class={chip}>
          <span class="truncate">@{m.title}</span>
          <button class={chipClear} aria-label={$t("conversation.unmention", { title: m.title })} title={$t("conversation.unmention", { title: m.title })} onclick={() => (mentioned = mentioned.filter((x) => x.id !== m.id))}><X size={12} /></button>
        </span>
      {/each}
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
  <div class="relative rounded-lg border border-input bg-field transition-colors hover:border-input-strong focus-within:border-primary/50 focus-within:ring-3 focus-within:ring-primary/10">
    {#if picking && (picking.kind === "mention" || choices.length > 0)}
      <div id={listId} class={cn(popover, "absolute right-0 bottom-full left-0 mb-1.5 max-h-72 overflow-y-auto")} role="listbox" aria-label={picking.kind === "mention" ? $t("conversation.mentionList") : $t("conversation.commandList")}>
        <p class={popoverLabel}>{picking.kind === "mention" ? $t("conversation.mentionList") : $t("conversation.commandList")}</p>
        {#each choices as choice, index (typeof choice === "string" ? choice : choice.id)}
          <button
            role="option"
            aria-selected={index === highlight}
            data-highlighted={index === highlight ? "" : undefined}
            class={cn(popoverItem, "min-w-0")}
            onmousedown={(event) => event.preventDefault()}
            onmouseenter={() => (highlight = index)}
            onclick={() => choose(choice)}
          >
            {#if typeof choice === "string"}
              <SquareTerminal size={14} class="shrink-0 text-muted" aria-hidden="true" />
              <span class="shrink-0 font-mono">/{choice}</span>
              <span class="min-w-0 truncate text-xs text-muted">{$t(`slash.${choice}`)}</span>
            {:else}
              <StateDot state={choice.state} />
              <span class="min-w-0 flex-1 truncate">{choice.title}</span>
              <span class="shrink-0 text-2xs text-muted">{choice.status === "closed" ? $t("conversation.mentionArchived") : choice.state ? $t(`task.state.${choice.state}`) : $t(`items.status.${choice.status}`)}</span>
            {/if}
          </button>
        {:else}
          <!-- Only an `@` opens the list with nothing in it. -->
          <p class="px-2 py-1 text-xs text-muted">{$t("conversation.mentionEmpty")}</p>
        {/each}
      </div>
    {/if}
    <input bind:this={fileRef} type="file" accept="image/*" multiple class="hidden" onchange={(event) => { const el = event.currentTarget as HTMLInputElement; void attach([...(el.files ?? [])]); el.value = ""; }} />
    <Textarea
      bind:ref={input}
      rows={2}
      bind:value={text}
      class="block border-0 bg-transparent px-3 pt-2.5 pb-1 hover:border-0 focus-visible:ring-0"
      {placeholder}
      aria-label={$t("conversation.input")}
      aria-controls={picking ? listId : undefined}
      oninput={syncPicker}
      onclick={syncPicker}
      onkeyup={(event) => { if (event.key === "ArrowLeft" || event.key === "ArrowRight" || event.key === "Home" || event.key === "End") syncPicker(); }}
      onblur={() => (picking = null)}
      onpaste={(event) => { const files = [...(event.clipboardData?.files ?? [])]; if (files.length > 0) { event.preventDefault(); void attach(files); } }}
      onkeydown={onKeydown}
    />
    <div class="flex items-center gap-1 px-1.5 pb-1.5">
      <Button variant="ghost" size="icon" aria-label={$t("chat.addImage")} onclick={() => fileRef?.click()}><ImagePlus /></Button>
      <Button variant="ghost" size="icon" aria-label={$t("conversation.mention")} title={$t("conversation.mention")} onmousedown={(event) => event.preventDefault()} onclick={startMention}><AtSign /></Button>
      <div class="min-w-0 flex-1">
        {#if addressees.length > 1}
          <p class="truncate px-1 text-xs text-muted">{$t("runAs.multiple")}</p>
        {:else}
          <!-- A choice still being saved belongs to the task it was made for, not the next one addressed. -->
          {#key only?.id}
            <RunAsBar
              value={only ? runAsOf(only.id) : draftRunAs}
              scope={only ? "task" : "new"}
              onchange={chooseRunAs}
              onconversation={only ? undefined : chooseConversation}
            />
          {/key}
        {/if}
      </div>
      <span class="relative flex">
      {#key sparks}{#if sparks > 0}<span class="pointer-events-none absolute inset-0 animate-spark rounded-md border-[1.5px] border-primary/70 shadow-ember-spark" aria-hidden="true"></span>{/if}{/key}
      <Button size="icon" onclick={submit} disabled={send.isPending || running || (text.trim().length === 0 && attached.length === 0)} aria-label={$t("common.send")}><ArrowUp /></Button>
      </span>
    </div>
  </div>
  </div>
</div>
