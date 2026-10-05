<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import LogOut from "@lucide/svelte/icons/log-out";
  import PanelLeft from "@lucide/svelte/icons/panel-left";
  import Search from "@lucide/svelte/icons/search";
  import Settings from "@lucide/svelte/icons/settings";
  import SquarePen from "@lucide/svelte/icons/square-pen";
  import { t } from "../i18n/index.js";
  import { api, type BoardCard } from "../lib/api.js";
  import { attentionCards, isUnread, STATE_DOT, trayCards } from "../lib/board.js";
  import { boot } from "../lib/boot.js";
  import { formatShortcut, isMacPlatform, shortcutKey } from "../lib/commands.js";
  import { atPointer, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import type { LiveState } from "../lib/live.js";
  import { MAIN_NAV, plainClick } from "../lib/nav.js";
  import { focusFrom, focusHref, navigate, openSettings, routeFor, routerState } from "../lib/router.svelte.js";
  import { seenState } from "../lib/seen.svelte.js";
  import { openTaskMenu } from "../lib/taskActions.js";
  import { setSidebarCollapsed, ui } from "../lib/ui.svelte.js";
  import { cn } from "../lib/utils.js";
  import BrandMark from "./BrandMark.svelte";
  import MoreButton from "./MoreButton.svelte";

  let { live, onsignout }: { live: LiveState; onsignout: () => void } = $props();

  const desktop = boot().desktop;
  // The desktop app has no token to drop, and its webview is never "disconnected" from its own host.
  const needsToken = boot().auth;
  const mac = isMacPlatform();
  const queryClient = useQueryClient();
  const boardQuery = createQuery(() => ({ queryKey: ["board"], queryFn: () => api.board() }));

  let route = $derived(routeFor(routerState.path));
  let focused = $derived(route.name === "chat" ? focusFrom(routerState.search) : null);
  let cards = $derived(trayCards(boardQuery.data?.cards ?? [], seenState.map));
  let attention = $derived(attentionCards(boardQuery.data?.cards ?? []));
  let liveLabel = $derived($t(live === "live" ? "live.live" : live === "connecting" ? "live.connecting" : "live.offline"));

  const keyHint = (command: Parameters<typeof shortcutKey>[0]) => {
    const key = shortcutKey(command);
    return key ? formatShortcut(key, mac) : "";
  };

  function active(path: string): boolean {
    if (path === "/") return route.name === "chat";
    return routerState.path === path || routerState.path.startsWith(`${path}/`);
  }

  function go(event: MouseEvent, path: string): void {
    if (!plainClick(event)) return;
    event.preventDefault();
    navigate(path);
  }

  function taskMenu(card: BoardCard, placement: MenuPlacement): void {
    const running = card.execution !== null && (card.state === "running" || card.state === "queued" || card.state === "thinking");
    openTaskMenu(queryClient, { id: card.item.id, title: card.item.title, running, status: card.item.status }, placement);
  }

  const row = "group flex w-full items-center gap-2.5 rounded-md px-2 py-1 text-left text-sm text-foreground/85 transition-colors duration-100 hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary/70";
  const kbd = "ml-auto font-sans text-2xs text-muted opacity-0 transition-opacity group-hover:opacity-100";
</script>

<aside class="flex h-full w-60 shrink-0 flex-col border-r border-border bg-surface" aria-label={$t("shell.sidebar")}>
  <!-- Desktop: the traffic lights sit in this row's left 78px. -->
  <div class={cn("drag-region flex h-[52px] shrink-0 items-center gap-2 px-3", desktop && "pl-[84px]")}>
    <span class={cn("flex items-center gap-2 text-sm font-semibold tracking-tight", desktop ? "px-0" : "px-1")}><BrandMark size={16} />Hidane</span>
    <button
      class="ml-auto flex size-7 items-center justify-center rounded-md text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70"
      aria-label={$t("shell.collapseSidebar")}
      title={`${$t("shell.collapseSidebar")} (${keyHint("toggle-sidebar")})`}
      onclick={() => setSidebarCollapsed(true)}
    >
      <PanelLeft size={16} aria-hidden="true" />
    </button>
  </div>

  <div class="space-y-0.5 px-2">
    <button class={row} onclick={() => (ui.newTaskOpen = true)} title={`${$t("shell.newTask")} (${keyHint("new-task")})`}>
      <SquarePen size={16} class="shrink-0 text-muted group-hover:text-foreground/85" aria-hidden="true" />{$t("shell.newTask")}<kbd class={kbd} aria-hidden="true">{keyHint("new-task")}</kbd>
    </button>
    <button class={row} onclick={() => (ui.paletteOpen = true)} title={`${$t("shell.search")} (${keyHint("search")})`}>
      <Search size={16} class="shrink-0 text-muted group-hover:text-foreground/85" aria-hidden="true" />{$t("shell.search")}<kbd class={kbd} aria-hidden="true">{keyHint("search")}</kbd>
    </button>
  </div>

  <nav class="mt-3 space-y-0.5 px-2" aria-label={$t("shell.mainNav")}>
    {#each MAIN_NAV as item (item.to)}
      {@const Icon = item.icon}
      <a
        href={item.to}
        class={cn(row, active(item.to) && "bg-accent text-foreground")}
        aria-current={active(item.to) ? "page" : undefined}
        onclick={(event) => go(event, item.to)}
      >
        <Icon size={16} class="shrink-0 text-muted group-hover:text-foreground/85" aria-hidden="true" />{$t(item.key)}
        {#if item.to === "/inbox" && attention.length > 0}
          <span class="ml-auto rounded-full bg-primary/12 px-1.5 text-2xs font-medium text-primary tabular-nums" aria-label={$t("shell.attentionCount", { count: attention.length })}>{attention.length}</span>
        {/if}
        <kbd class={cn(kbd, item.to === "/inbox" && attention.length > 0 && "ml-1")} aria-hidden="true">{keyHint(item.command)}</kbd>
      </a>
    {/each}
  </nav>

  {#snippet taskRow(card: BoardCard, unread: boolean)}
    <li class="group relative">
      <button
        class={cn(row, "pr-8", card.item.id === focused && "bg-accent text-foreground")}
        aria-current={card.item.id === focused ? "true" : undefined}
        onclick={() => navigate(focusHref(card.item.id))}
        oncontextmenu={(event) => { event.preventDefault(); taskMenu(card, atPointer(event)); }}
      >
        <span aria-hidden="true" class={cn("mx-[5px] size-1.5 shrink-0 rounded-full", STATE_DOT[card.state])}></span>
        <span class="min-w-0 flex-1 truncate">{card.item.title}</span>
        <span class="sr-only">{$t(`task.state.${card.state}`)}</span>
        {#if unread}<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary" title={$t("task.unread")}></span><span class="sr-only">{$t("task.unread")}</span>{/if}
      </button>
      <MoreButton
        class="absolute top-1/2 right-1.5 -translate-y-1/2 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
        label={$t("menu.moreFor", { title: card.item.title })}
        onopen={(placement) => taskMenu(card, placement)}
      />
    </li>
  {/snippet}

  <div class="mt-4 min-h-0 flex-1 space-y-3 overflow-y-auto overscroll-contain pb-2">
    {#if attention.length > 0}
      <section aria-labelledby="sidebar-attention">
        <h2 id="sidebar-attention" class="px-4 pb-1 text-2xs font-medium text-muted">{$t("task.attention")}</h2>
        <ul class="space-y-0.5 px-2">
          {#each attention as card (card.item.id)}{@render taskRow(card, false)}{/each}
        </ul>
      </section>
    {/if}
    <section aria-labelledby="sidebar-progress">
      <h2 id="sidebar-progress" class="px-4 pb-1 text-2xs font-medium text-muted">{$t("task.tray")}</h2>
      <ul class="space-y-0.5 px-2">
        {#each cards as card (card.item.id)}
          {@render taskRow(card, card.item.id !== focused && isUnread(card, seenState.map))}
        {:else}
          <li class="px-2 py-1 text-xs text-muted">{$t("shell.nothingInProgress")}</li>
        {/each}
      </ul>
    </section>
  </div>

  <div class="shrink-0 space-y-0.5 border-t border-border p-2">
    <button class={row} onclick={() => openSettings()} title={`${$t("shell.settings")} (${keyHint("open-settings")})`}>
      <Settings size={16} class="shrink-0 text-muted group-hover:text-foreground/85" aria-hidden="true" />{$t("shell.settings")}<kbd class={kbd} aria-hidden="true">{keyHint("open-settings")}</kbd>
    </button>
    {#if !desktop}
      <div class="flex items-center gap-1 px-1">
        <span class="flex min-w-0 flex-1 items-center gap-2 px-1.5 py-1 text-xs text-muted" title={$t("live.hint")} role="status" aria-label={`${$t("live.hint")}: ${liveLabel}`}>
          <span aria-hidden="true" class={cn("h-2 w-2 shrink-0 rounded-full", live === "live" ? "bg-success" : live === "connecting" ? "animate-ember bg-primary" : "bg-danger")}></span>
          <span aria-hidden="true" class="truncate">{liveLabel}</span>
        </span>
        {#if needsToken}
          <button
            class="flex size-7 items-center justify-center rounded-md text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70"
            aria-label={$t("token.signOut")}
            title={$t("token.signOut")}
            onclick={onsignout}
          >
            <LogOut size={15} aria-hidden="true" />
          </button>
        {/if}
      </div>
    {/if}
  </div>
</aside>
