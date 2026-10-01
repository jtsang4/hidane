<script lang="ts">
  import { Ban, CircleHelp, Maximize2, Square } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import type { BoardCard } from "../lib/api.js";
  import { stateTone } from "../lib/board.js";
  import { liveRepliesFor } from "../lib/liveText.js";
  import { cn } from "../lib/utils.js";
  import Markdown from "./Markdown.svelte";
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
  } = $props();

  let busy = $derived(card.state === "running" || card.state === "queued" || card.state === "thinking");
  /** The Manager's reply while it is still being written. */
  let live = $derived(liveRepliesFor(card.item.threadId, card.lastReply?.seq ?? 0));
  let children = $derived(card.childIds.map((id) => cards.get(id)).filter((c): c is BoardCard => c !== undefined));
  const dot: Record<string, string> = {
    waiting: "bg-danger",
    running: "animate-pulse bg-primary",
    queued: "bg-primary/50",
    thinking: "animate-pulse bg-primary/70",
    delegated: "animate-pulse bg-primary/50",
    idle: "bg-muted",
    done: "bg-success",
    closed: "bg-muted",
  };
</script>

<article
  class={cn(
    "w-full rounded-lg border bg-surface p-3 text-sm",
    !headless && "max-w-[85%]",
    card.state === "waiting" ? "border-danger/50" : focused ? "border-primary/60" : "border-border",
  )}
  aria-label={card.item.title}
>
  {#if headless}
    {#if card.understanding}
      <p class="text-xs text-muted"><span class="mr-1 rounded bg-surface-2 px-1 py-px text-[10px] text-foreground/80">{$t("task.understanding")}</span>{card.understanding}</p>
    {/if}
  {:else}
  <header class="flex items-start gap-2">
    <span aria-hidden="true" class={cn("mt-1.5 h-2 w-2 shrink-0 rounded-full", dot[card.state])}></span>
    <div class="min-w-0 flex-1">
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-medium break-words">{card.item.title}</span>
        <Badge tone={stateTone(card.state)}>{$t(`task.state.${card.state}`)}</Badge>
      </div>
      {#if card.understanding}
        <p class="mt-1 text-xs text-muted"><span class="mr-1 rounded bg-surface-2 px-1 py-px text-[10px] text-foreground/80">{$t("task.understanding")}</span>{card.understanding}</p>
      {/if}
    </div>
    <div class="flex shrink-0 items-center gap-1">
      {#if busy && card.execution}
        <Button variant="ghost" size="icon" aria-label={$t("task.stop")} title={$t("task.stop")} onclick={() => onstop(card.item.id)}><Square size={14} /></Button>
      {/if}
      {#if !focused}
        <Button variant="ghost" size="icon" aria-label={$t("task.open")} title={$t("task.open")} onclick={() => onfocus(card.item.id)}><Maximize2 size={14} /></Button>
      {/if}
    </div>
  </header>
  {/if}

  {#if card.execution}
    <p class="mt-2 truncate first:mt-0 text-xs text-muted">
      {$t(`task.state.${card.execution.status === "running" ? "running" : "queued"}`)} · {$t("task.progress", { n: card.execution.toolCalls })}{#if card.execution.lastTool} · {$t("task.lastTool", { tool: card.execution.lastTool })}{/if}
    </p>
  {/if}

  {#each live as reply (reply.id)}
    <div class="mt-2 rounded-md bg-surface-2 px-2 py-1.5"><Markdown content={reply.text} /></div>
  {/each}

  {#if card.escalation && compact}
    <div class="mt-2 flex items-center gap-2 rounded-md border border-danger/40 bg-danger/5 px-2 py-1.5">
      <CircleHelp size={14} class="shrink-0 text-danger" aria-hidden="true" />
      <p class="min-w-0 flex-1 truncate text-xs"><span class="font-medium text-danger">{$t("task.question")}</span> · {card.escalation.question}</p>
      <Button size="sm" onclick={() => onanswer(card)}>{$t("task.answer")}</Button>
    </div>
  {:else if card.escalation}
    <div class="mt-2 rounded-md border border-danger/40 bg-danger/5 p-2">
      <p class="flex items-start gap-1.5 text-xs font-medium text-danger"><CircleHelp size={14} class="mt-px shrink-0" />{$t("task.question")}</p>
      <p class="mt-1 whitespace-pre-wrap">{card.escalation.question}</p>
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
      <div class="mt-2"><Button size="sm" onclick={() => onanswer(card)}>{$t("task.answer")}</Button></div>
    </div>
  {/if}

  {#if card.lastPolicyBlock && busy}
    <p class="mt-2 flex items-start gap-1.5 text-xs text-danger"><Ban size={12} class="mt-0.5 shrink-0" />{$t("task.policyBlocked", { reason: card.lastPolicyBlock.reason })}</p>
  {/if}

  {#if children.length > 0}
    <div class="mt-2 flex flex-wrap items-center gap-1.5 text-xs">
      <span class="text-muted">{$t("task.children")}</span>
      {#each children as child (child.item.id)}
        <button class="flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 hover:text-foreground" onclick={() => onfocus(child.item.id)}>
          <span aria-hidden="true" class={cn("h-1.5 w-1.5 rounded-full", dot[child.state])}></span>
          {child.item.title}
          <span class="sr-only">{$t(`task.state.${child.state}`)}</span>
        </button>
      {/each}
    </div>
  {/if}
</article>
