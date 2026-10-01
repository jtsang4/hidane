<script lang="ts">
  import { t } from "../i18n/index.js";
  import type { BoardCard } from "../lib/api.js";
  import { isUnread } from "../lib/board.js";
  import { cn } from "../lib/utils.js";

  let {
    cards,
    seen,
    focused,
    onfocus,
  }: {
    cards: BoardCard[];
    seen: Readonly<Record<string, number>>;
    focused: string | null;
    onfocus: (id: string) => void;
  } = $props();

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

{#if cards.length > 0}
  <nav class="flex items-center gap-1.5 overflow-x-auto border-b border-border px-2 py-1.5" aria-label={$t("task.tray")}>
    <span class="shrink-0 px-1 text-xs text-muted">{$t("task.tray")}</span>
    {#each cards as card (card.item.id)}
      {@const unread = card.item.id !== focused && isUnread(card, seen)}
      <button
        class={cn(
          "flex max-w-[14rem] shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
          card.item.id === focused ? "border-primary/60 bg-primary/10 text-foreground" : "border-border text-muted hover:text-foreground",
        )}
        aria-current={card.item.id === focused ? "true" : undefined}
        onclick={() => onfocus(card.item.id)}
      >
        <span aria-hidden="true" class={cn("h-2 w-2 shrink-0 rounded-full", dot[card.state])}></span>
        <span class="truncate">{card.item.title}</span>
        <span class="sr-only">{$t(`task.state.${card.state}`)}</span>
        {#if unread}<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" title={$t("task.unread")}></span><span class="sr-only">{$t("task.unread")}</span>{/if}
      </button>
    {/each}
  </nav>
{/if}
