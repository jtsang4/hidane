<script lang="ts" module>
  import type { Component } from "svelte";

  export interface PaletteCommand {
    id: string;
    label: string;
    keywords: string[];
    group: "go" | "action" | "settings";
    /** Shown on the right, e.g. "⌘K". */
    shortcut?: string;
    icon?: Component<{ size?: number; class?: string }>;
    run: () => void;
  }

  export type PaletteSearch = (query: string) => Promise<{ events: HidaneEvent[]; items: WorkItem[]; titles: Record<string, string> }>;

  /** Reopening the palette offers the last search again — the way back to its results. */
  let lastQuery = "";
</script>

<script lang="ts">
  import { tick } from "svelte";
  import { Dialog } from "bits-ui";
  import { CornerDownLeft, MessageSquareText, Search, SquareCheckBig } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type HidaneEvent, type WorkItem } from "../lib/api.js";
  import { excerpt, highlight, saidText, searchTerms } from "../lib/history.js";
  import { filterCommands, stepSelection } from "../lib/palette.js";
  import { dialogOverlay, dialogSurface } from "../lib/styles.js";
  import { cn, fmtDateTime } from "../lib/utils.js";

  let {
    commands,
    onclose,
    onopenitem,
    onopenmessage,
    search = (query: string) => api.searchConversation(query, undefined, 12),
    debounceMs = 180,
  }: {
    commands: PaletteCommand[];
    onclose: () => void;
    onopenitem: (id: string) => void;
    onopenmessage: (eventId: string) => void;
    search?: PaletteSearch;
    debounceMs?: number;
  } = $props();

  type Option =
    | { key: string; kind: "command"; command: PaletteCommand }
    | { key: string; kind: "item"; item: WorkItem }
    | { key: string; kind: "message"; event: HidaneEvent };

  const listId = $props.id();
  let query = $state(lastQuery);
  let results = $state.raw<Awaited<ReturnType<PaletteSearch>> | null>(null);
  let searching = $state(false);
  let failed = $state<string | null>(null);
  let list = $state<HTMLDivElement | undefined>();
  let input = $state<HTMLInputElement | undefined>();

  let trimmed = $derived(query.trim());
  let terms = $derived(searchTerms(trimmed));
  let commandHits = $derived(filterCommands(commands, trimmed));
  let items = $derived(trimmed && results ? results.items : []);
  let hits = $derived(trimmed && results ? results.events : []);
  let options = $derived<Option[]>([
    ...commandHits.map((command) => ({ key: `c-${command.id}`, kind: "command" as const, command })),
    ...items.map((item) => ({ key: `i-${item.id}`, kind: "item" as const, item })),
    ...hits.map((event) => ({ key: `m-${event.id}`, kind: "message" as const, event })),
  ]);
  let selected = $derived.by(() => {
    void trimmed;
    return 0;
  });

  // Debounced: each search reads the whole log.
  $effect(() => {
    const current = trimmed;
    lastQuery = query;
    if (!current) {
      results = null;
      searching = false;
      failed = null;
      return;
    }
    searching = true;
    let stale = false;
    const timer = window.setTimeout(() => {
      search(current)
        .then((found) => {
          if (stale) return;
          results = found;
          failed = null;
        })
        .catch((error: unknown) => {
          if (!stale) failed = error instanceof Error ? error.message : String(error);
        })
        .finally(() => {
          if (!stale) searching = false;
        });
    }, debounceMs);
    return () => {
      stale = true;
      window.clearTimeout(timer);
    };
  });

  $effect(() => {
    const index = selected;
    list?.querySelector<HTMLElement>(`[data-index="${index}"]`)?.scrollIntoView?.({ block: "nearest" });
  });

  function choose(index: number): void {
    const option = options[index];
    if (!option) return;
    onclose();
    // After the palette has gone and given focus back, so the action can take it.
    void tick().then(() => {
      if (option.kind === "command") option.command.run();
      else if (option.kind === "item") onopenitem(option.item.id);
      else onopenmessage(option.event.id);
    });
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.isComposing) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      selected = stepSelection(selected, event.key === "ArrowDown" ? 1 : -1, options.length);
    } else if (event.key === "Enter") {
      event.preventDefault();
      choose(selected);
    }
  }

  function who(event: HidaneEvent): string {
    if (event.kind === "user.message") return $t("chat.who.person");
    if (event.kind === "escalation") return $t("chat.who.question");
    return $t("chat.who.assistant");
  }

  function itemTitle(id: string): string {
    return results?.titles[id] ?? id;
  }

  const firstOf = (kind: Option["kind"]) => options.findIndex((option) => option.kind === kind);
</script>

{#snippet marked(text: string)}
  {#each highlight(text, terms) as part, i (i)}{#if part.hit}<mark class="rounded-sm bg-primary/25 text-foreground">{part.text}</mark>{:else}{part.text}{/if}{/each}
{/snippet}

{#snippet heading(id: string, text: string)}
  <div id={id} class="px-3.5 pt-2.5 pb-1 text-2xs font-medium text-muted" role="presentation">{text}</div>
{/snippet}

<!-- Mounted while open by App; Esc or a click outside closes it. -->
<Dialog.Root bind:open={() => true, (open) => { if (!open) onclose(); }}>
  <Dialog.Portal>
    <Dialog.Overlay class={cn(dialogOverlay, "z-[60]")} />
    <Dialog.Content
      aria-label={$t("palette.label")}
      class={cn(dialogSurface, "fixed top-[12vh] left-1/2 z-[60] flex max-h-[70vh] w-[640px] max-w-[calc(100vw-2rem)] -translate-x-1/2 flex-col overflow-hidden")}
      onOpenAutoFocus={(event) => {
        // The last search is offered again, selected, so typing replaces it.
        event.preventDefault();
        input?.focus();
        input?.select();
      }}
    >
      <div class="flex items-center gap-2 border-b border-border px-3">
        <Search size={14} class="shrink-0 text-muted" aria-hidden="true" />
        <input
          bind:this={input}
          bind:value={query}
          class="h-11 min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-muted/70"
          placeholder={$t("palette.placeholder")}
          aria-label={$t("palette.placeholder")}
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={options[selected] ? `${listId}-${selected}` : undefined}
          autocomplete="off"
          spellcheck="false"
          {onkeydown}
        />
        {#if searching}<span class="h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent" role="status" aria-label={$t("chat.searching")}></span>{/if}
      </div>
      <div bind:this={list} id={listId} role="listbox" aria-label={$t("palette.label")} class="min-h-0 flex-1 overflow-y-auto overscroll-contain py-1">
        {#each options as option, index (option.key)}
          {#if index === firstOf("command")}{@render heading(`${listId}-h-c`, $t("palette.commands"))}{/if}
          {#if index === firstOf("item")}{@render heading(`${listId}-h-i`, $t("palette.items"))}{/if}
          {#if index === firstOf("message")}{@render heading(`${listId}-h-m`, $t("palette.messages"))}{/if}
          <div
            id={`${listId}-${index}`}
            data-index={index}
            role="option"
            tabindex="-1"
            aria-selected={index === selected}
            class={cn("mx-1.5 flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm", index === selected ? "bg-accent text-foreground" : "text-foreground/90")}
            onmousemove={() => { if (selected !== index) selected = index; }}
            onclick={() => choose(index)}
            onkeydown={(event) => { if (event.key === "Enter") choose(index); }}
          >
            {#if option.kind === "command"}
              {@const Icon = option.command.icon}
              {#if Icon}<Icon size={14} class="shrink-0 text-muted" />{/if}
              <span class="min-w-0 flex-1 truncate">{@render marked(option.command.label)}</span>
              {#if option.command.shortcut}<kbd class="shrink-0 font-sans text-2xs text-muted">{option.command.shortcut}</kbd>{/if}
            {:else if option.kind === "item"}
              <SquareCheckBig size={14} class="shrink-0 text-muted" aria-hidden="true" />
              <span class="min-w-0 flex-1 truncate">{@render marked(option.item.title)}</span>
              <span class="shrink-0 font-mono text-2xs text-muted">{option.item.id}</span>
            {:else}
              {@const hit = option.event}
              <MessageSquareText size={14} class="mt-0.5 shrink-0 self-start text-muted" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="flex items-center gap-2 text-2xs text-muted">
                  <span class="font-medium text-foreground/80">{who(hit)}</span>
                  {#if hit.workItemId}<span class="min-w-0 truncate">· {itemTitle(hit.workItemId)}</span>{/if}
                  <time class="ml-auto shrink-0" dateTime={hit.ts}>{fmtDateTime(hit.ts)}</time>
                </span>
                <span class="mt-0.5 block truncate">{@render marked(excerpt(saidText(hit), terms))}</span>
              </span>
            {/if}
            {#if index === selected}<CornerDownLeft size={13} class="shrink-0 text-muted max-sm:hidden" aria-hidden="true" />{/if}
          </div>
        {/each}
        {#if options.length === 0}
          <p class="px-4 py-8 text-center text-sm text-muted">{searching ? $t("chat.searching") : failed ?? $t("palette.empty")}</p>
        {/if}
      </div>
      <div class="flex items-center gap-3 border-t border-border px-3 py-1.5 text-2xs text-muted coarse:hidden max-sm:hidden">
        <!-- The hint reads "keys label · keys label": each key becomes a keycap. -->
        {#each $t("palette.hint").split(" · ") as hint (hint)}
          {@const gap = hint.indexOf(" ")}
          <span class="flex items-center gap-1.5"><kbd class="rounded-sm border border-border bg-accent px-1 font-sans text-2xs leading-4 text-foreground/80">{hint.slice(0, gap)}</kbd>{hint.slice(gap + 1)}</span>
        {/each}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
