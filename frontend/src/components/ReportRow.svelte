<script lang="ts">
  import { t } from "../i18n/index.js";
  import type { CardState, HidaneEvent } from "../lib/api.js";
  import { STATE_DOT } from "../lib/board.js";
  import { atPointer, keepsSystemMenu, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { payloadText } from "../lib/grouping.js";
  import { cn } from "../lib/utils.js";
  import ChatBubble from "./ChatBubble.svelte";
  import MoreButton from "./MoreButton.svelte";
  import Time from "./Time.svelte";

  let {
    event,
    title,
    cardState = null,
    onfocus,
    onmenu,
  }: {
    /** A task's report: its work came back on its own, not as an answer to what was just said. */
    event: HidaneEvent;
    title: string;
    cardState?: CardState | null;
    onfocus: (id: string) => void;
    onmenu: (event: HidaneEvent, placement: MenuPlacement) => void;
  } = $props();

  let open = $state(false);
  /** The first line that says something, without Markdown's leading marks. */
  let summary = $derived.by(() => {
    for (const line of payloadText(event).split("\n")) {
      const plain = line.replace(/^[\s#>*\-+\d.)`]+/, "").replace(/[*_`]/g, "").trim();
      if (plain) return plain.length > 160 ? `${plain.slice(0, 160)}…` : plain;
    }
    return "";
  });

  function contextMenu(e: MouseEvent): void {
    if (keepsSystemMenu(e.currentTarget as Element)) return;
    e.preventDefault();
    onmenu(event, atPointer(e));
  }
</script>

<div class="group/answer relative space-y-1" role="presentation" oncontextmenu={contextMenu}>
  <div id={open ? undefined : `ev-${event.id}`} class="flex max-w-[85%] min-w-0 items-center gap-2 text-xs">
    <span aria-hidden="true" class={cn("size-1.5 shrink-0 rounded-full", cardState ? STATE_DOT[cardState] : "bg-muted")}></span>
    <button class="max-w-[40%] shrink-0 truncate font-medium text-foreground/85 hover:underline" onclick={() => event.workItemId && onfocus(event.workItemId)}>{title}</button>
    {#if !open}<span class="min-w-0 flex-1 truncate text-muted select-text">{summary}</span>{/if}
    <button class="shrink-0 rounded-sm px-1 text-primary hover:bg-primary/10" aria-expanded={open} onclick={() => (open = !open)}>
      {open ? $t("conversation.hideReport") : $t("conversation.showReport")}
    </button>
    <Time iso={event.ts} class="shrink-0 text-2xs text-muted" />
    <MoreButton
      class="opacity-0 transition-opacity group-hover/answer:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
      label={$t("menu.messageMore")}
      onopen={(placement) => onmenu(event, placement)}
    />
  </div>
  {#if open}<ChatBubble {event} anchored />{/if}
</div>
