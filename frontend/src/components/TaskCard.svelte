<script lang="ts">
  import { Ban, Bot, CircleHelp, GitBranch, Maximize2, Square } from "@lucide/svelte";
  import { prefersReducedMotion } from "svelte/motion";
  import { fade } from "svelte/transition";
  import { t } from "../i18n/index.js";
  import type { BoardCard } from "../lib/api.js";
  import { stateTone } from "../lib/board.js";
  import { atPointer, keepsSystemMenu, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { escalationText } from "../lib/escalation.js";
  import { liveRepliesFor } from "../lib/liveText.js";
  import { runAsSummary } from "../lib/runAs.js";
  import { cn } from "../lib/utils.js";
  import Markdown from "./Markdown.svelte";
  import MoreButton from "./MoreButton.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";

  let {
    card,
    cards,
    focused = false,
    compact = false,
    headless = false,
    onfocus,
    onanswer,
    onstop,
    onmenu,
  }: {
    card: BoardCard;
    /** Every card, to name this one's children. */
    cards: ReadonlyMap<string, BoardCard>;
    focused?: boolean;
    /** In the conversation the question is shown in full where it was asked; one line here. */
    compact?: boolean;
    /** Inside the focus panel, whose header already names the item and its state. */
    headless?: boolean;
    onfocus: (id: string) => void;
    onanswer: (card: BoardCard) => void;
    onstop: (id: string) => void;
    /** The task menu — from a right click on the card or its "⋯" button. */
    onmenu?: ((card: BoardCard, placement: MenuPlacement) => void) | undefined;
  } = $props();

  let busy = $derived(card.state === "running" || card.state === "queued" || card.state === "thinking");
  /** The Manager's reply while it is still being written. */
  let live = $derived(liveRepliesFor(card.item.threadId, card.lastReply?.seq ?? 0));
  let question = $derived(card.escalation ? escalationText({ reason: card.escalation.reason, question: card.escalation.question }) : "");
  let children = $derived(card.childIds.map((id) => cards.get(id)).filter((c): c is BoardCard => c !== undefined));
  /** Where the task's work lives: each repository and the branch it is on. */
  let checkouts = $derived(card.checkouts.filter((c) => c.status === "active"));
  const dot: Record<string, string> = {
    waiting: "bg-danger",
    running: "animate-ember bg-primary",
    queued: "bg-primary/50",
    thinking: "animate-ember bg-primary/80",
    delegated: "animate-ember bg-primary/60",
    idle: "bg-muted",
    done: "bg-success",
    closed: "bg-muted",
  };
</script>

<article
  class={cn(
    "relative w-full rounded-lg border bg-surface px-3 py-2.5 text-sm",
    !headless && "max-w-[85%]",
    card.state === "waiting" ? "border-danger/40" : focused ? "border-primary/35 shadow-ember-glow" : "border-border",
  )}
  aria-label={card.item.title}
  oncontextmenu={(event) => {
    if (!onmenu || headless) return;
    // Selected text keeps the system menu, so it can be copied.
    if (keepsSystemMenu(event.currentTarget)) return;
    event.preventDefault();
    onmenu(card, atPointer(event));
  }}
>
  <!-- While it runs the card smolders; when it stops, the light cools away. -->
  {#if busy}
    <span class="pointer-events-none absolute inset-x-0 -top-px h-px overflow-hidden rounded-t-lg" aria-hidden="true" out:fade={{ duration: prefersReducedMotion.current ? 0 : 700 }}>
      <span class="absolute inset-y-0 left-0 w-2/5 animate-smolder bg-linear-to-r from-transparent via-primary to-transparent shadow-ember-spark"></span>
    </span>
  {/if}
  {#if headless}
    {#if card.understanding}
      <p class="flex items-start gap-1.5 text-xs text-muted"><span class="mt-px shrink-0 rounded-sm bg-accent px-1 text-2xs text-foreground/80">{$t("task.understanding")}</span><span class="min-w-0">{card.understanding}</span></p>
    {/if}
  {:else}
  <header class="flex items-start gap-2">
    <span aria-hidden="true" class={cn("mt-[7px] size-1.5 shrink-0 rounded-full", dot[card.state])}></span>
    <div class="min-w-0 flex-1">
      <!-- The state keeps to the title's first line; a long title wraps in its own column. -->
      <div class="flex items-start gap-2">
        <span class="min-w-0 font-medium break-words">{card.item.title}</span>
        <Badge class="mt-px shrink-0" tone={stateTone(card.state)}>{$t(`task.state.${card.state}`)}</Badge>
      </div>
      {#if card.item.runAs}
        <p class="mt-0.5 flex items-center gap-1 text-xs text-muted" title={$t("runAs.label")}>
          <Bot size={12} aria-hidden="true" />{runAsSummary(card.item.runAs, (effort) => $t(`settings.effort.${effort || "default"}`), $t("runAs.defaultModel"))}
        </p>
      {/if}
      {#if card.understanding}
        <p class="mt-1 flex items-start gap-1.5 text-xs text-muted"><span class="mt-px shrink-0 rounded-sm bg-accent px-1 text-2xs text-foreground/80">{$t("task.understanding")}</span><span class="min-w-0">{card.understanding}</span></p>
      {/if}
    </div>
    <div class="-my-0.5 -mr-1 flex shrink-0 items-center gap-0.5">
      {#if busy && card.execution}
        <Button variant="ghost" size="icon-sm" aria-label={$t("task.stop")} title={$t("task.stop")} onclick={() => onstop(card.item.id)}><Square size={14} /></Button>
      {/if}
      {#if !focused}
        <Button variant="ghost" size="icon-sm" aria-label={$t("task.open")} title={$t("task.open")} onclick={() => onfocus(card.item.id)}><Maximize2 size={14} /></Button>
      {/if}
      {#if onmenu}
        <MoreButton label={$t("menu.moreFor", { title: card.item.title })} onopen={(placement) => onmenu(card, placement)} />
      {/if}
    </div>
  </header>
  {/if}

  {#if checkouts.length > 0}
    <div class="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 first:mt-0">
      {#each checkouts as c (c.id)}
        <p class={cn("flex min-w-0 items-center gap-1 text-xs", c.missing ? "text-danger" : "text-muted")} title={$t("worktrees.branchOf", { repo: c.repo, branch: c.branch })}>
          <GitBranch size={12} class="shrink-0" aria-hidden="true" /><span class="shrink-0">{c.repo}</span><span class="truncate font-mono">{c.branch}</span>
          {#if c.continues}<span class="truncate">· {$t("worktrees.continues", { branch: c.continues })}</span>{/if}
          {#if c.mode === "in_place"}<span class="shrink-0">· {$t("worktrees.inPlace")}</span>{/if}
          {#if c.missing}<span class="shrink-0">· {$t("worktrees.status.repoMissing")}</span>{:else if c.setup === "running"}<span class="shrink-0">· {$t("worktrees.status.setup")}</span>{:else if c.setup === "failed"}<span class="shrink-0 text-danger">· {$t("worktrees.status.setupFailed")}</span>{/if}
        </p>
      {/each}
    </div>
  {/if}

  {#if card.execution}
    <p class="mt-2 truncate first:mt-0 text-xs text-muted">
      {$t(`task.state.${card.execution.status === "running" ? "running" : "queued"}`)} · {$t("task.progress", { count: card.execution.toolCalls })}{#if card.execution.lastTool}{` · ${$t("task.lastTool", { tool: card.execution.lastTool })}`}{/if}
    </p>
  {/if}

  {#each live as reply (reply.id)}
    <div class="mt-2 rounded-md bg-accent px-2 py-1.5"><Markdown content={reply.text} class="select-text" /></div>
  {/each}

  {#if card.escalation && compact}
    <div class="mt-2 flex items-center gap-2 rounded-md bg-danger/8 py-1 pr-1 pl-2">
      <CircleHelp size={13} class="shrink-0 text-danger" aria-hidden="true" />
      <p class="min-w-0 flex-1 truncate text-xs"><span class="font-medium text-danger">{$t("task.question")}</span> · {question}</p>
      <Button size="sm" onclick={() => onanswer(card)}>{$t("task.answer")}</Button>
    </div>
  {:else if card.escalation}
    <div class="mt-2 rounded-md bg-danger/8 p-2">
      <p class="flex items-start gap-1.5 text-xs font-medium text-danger"><CircleHelp size={13} class="mt-px shrink-0" />{$t("task.question")}</p>
      <p class="mt-1 whitespace-pre-wrap select-text">{question}</p>
      {#if card.escalation.path.some((step) => step.tried)}
        <details class="mt-1 text-xs text-muted">
          <summary class="cursor-pointer">{$t("task.tried")}</summary>
          <ul class="mt-1 list-disc pl-4">
            {#each card.escalation.path.filter((step) => step.tried) as step (step.workItemId)}
              <li><span class="text-foreground/80">{step.title}</span> — {step.tried}</li>
            {/each}
          </ul>
        </details>
      {/if}
      <div class="mt-2"><Button onclick={() => onanswer(card)}>{$t("task.answer")}</Button></div>
    </div>
  {/if}

  {#if card.lastPolicyBlock && busy}
    <p class="mt-2 flex items-start gap-1.5 text-xs text-danger"><Ban size={12} class="mt-0.5 shrink-0" />{$t("task.policyBlocked", { reason: card.lastPolicyBlock.reason })}</p>
  {/if}

  {#if children.length > 0}
    <div class="mt-2 flex flex-wrap items-center gap-1.5 text-xs">
      <span class="text-muted">{$t("task.children")}</span>
      {#each children as child (child.item.id)}
        <button class="flex h-5 items-center gap-1 rounded-md bg-accent px-1.5 hover:bg-accent-strong hover:text-foreground" onclick={() => onfocus(child.item.id)}>
          <span aria-hidden="true" class={cn("h-1.5 w-1.5 rounded-full", dot[child.state])}></span>
          {child.item.title}
          <span class="sr-only">{$t(`task.state.${child.state}`)}</span>
        </button>
      {/each}
    </div>
  {/if}
</article>
