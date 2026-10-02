<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Flame, LogOut, PanelLeft, Search, Settings, SquarePen } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type BoardCard } from "../lib/api.js";
  import { isUnread, trayCards } from "../lib/board.js";
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
  let liveLabel = $derived($t(live === "live" ? "live.live" : live === "connecting" ? "live.connecting" : "live.offline"));

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

  const row = "group flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm text-foreground/85 hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary";
  const kbd = "ml-auto font-sans text-[11px] text-muted opacity-0 transition-opacity group-hover:opacity-100";
</script>

<aside class="flex h-full w-60 shrink-0 flex-col border-r border-border bg-surface" aria-label={$t("shell.sidebar")}>
  <!-- Desktop: the traffic lights sit in this row's left 78px. -->
  <div class={cn("drag-region flex h-[52px] shrink-0 items-center gap-2 px-3", desktop && "pl-[84px]")}>
    {#if !desktop}
      <span class="flex items-center gap-2 px-1 text-sm font-semibold"><Flame size={18} class="text-primary" aria-hidden="true" />hidane</span>
    {/if}
    <button
      class="ml-auto flex h-7 w-7 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
      aria-label={$t("shell.collapseSidebar")}
      title={`${$t("shell.collapseSidebar")} (${keyHint("toggle-sidebar")})`}
      onclick={() => setSidebarCollapsed(true)}
    >
      <PanelLeft size={16} aria-hidden="true" />
    </button>
  </div>

  <div class="space-y-0.5 px-2">
    <button class={row} onclick={() => (ui.newTaskOpen = true)} title={`${$t("shell.newTask")} (${keyHint("new-task")})`}>
      <SquarePen size={16} class="shrink-0 text-muted" aria-hidden="true" />{$t("shell.newTask")}<kbd class={kbd} aria-hidden="true">{keyHint("new-task")}</kbd>
    </button>
    <button class={row} onclick={() => (ui.paletteOpen = true)} title={`${$t("shell.search")} (${keyHint("search")})`}>
      <Search size={16} class="shrink-0 text-muted" aria-hidden="true" />{$t("shell.search")}<kbd class={kbd} aria-hidden="true">{keyHint("search")}</kbd>
    </button>
  </div>

  <nav class="mt-3 space-y-0.5 px-2" aria-label={$t("shell.mainNav")}>
    {#each MAIN_NAV as item (item.to)}
      {@const Icon = item.icon}
      <a
        href={item.to}
        class={cn(row, active(item.to) && "bg-surface-2 text-foreground")}
        aria-current={active(item.to) ? "page" : undefined}
        onclick={(event) => go(event, item.to)}
      >
        <Icon size={16} class="shrink-0 text-muted" aria-hidden="true" />{$t(item.key)}<kbd class={kbd} aria-hidden="true">{keyHint(item.command)}</kbd>
      </a>
    {/each}
  </nav>

  <section class="mt-4 flex min-h-0 flex-1 flex-col" aria-labelledby="sidebar-progress">
    <h2 id="sidebar-progress" class="px-4 pb-1 text-[11px] font-medium text-muted">{$t("task.tray")}</h2>
    <ul class="min-h-0 flex-1 space-y-0.5 overflow-y-auto overscroll-contain px-2 pb-2">
      {#each cards as card (card.item.id)}
        {@const unread = card.item.id !== focused && isUnread(card, seenState.map)}
        <li class="group relative">
          <button
            class={cn(row, "pr-8", card.item.id === focused && "bg-surface-2 text-foreground")}
            aria-current={card.item.id === focused ? "true" : undefined}
            onclick={() => navigate(focusHref(card.item.id))}
            oncontextmenu={(event) => { event.preventDefault(); taskMenu(card, atPointer(event)); }}
          >
            <span aria-hidden="true" class={cn("h-2 w-2 shrink-0 rounded-full", dot[card.state])}></span>
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
      {:else}
        <li class="px-2.5 py-1.5 text-xs text-muted">{$t("shell.nothingInProgress")}</li>
      {/each}
    </ul>
  </section>

  <div class="shrink-0 space-y-0.5 border-t border-border p-2">
    <button class={row} onclick={() => openSettings()} title={`${$t("shell.settings")} (${keyHint("open-settings")})`}>
      <Settings size={16} class="shrink-0 text-muted" aria-hidden="true" />{$t("shell.settings")}<kbd class={kbd} aria-hidden="true">{keyHint("open-settings")}</kbd>
    </button>
    {#if !desktop}
      <div class="flex items-center gap-1 px-1">
        <span class="flex min-w-0 flex-1 items-center gap-2 px-1.5 py-1 text-xs text-muted" title={$t("live.hint")} role="status" aria-label={`${$t("live.hint")}: ${liveLabel}`}>
          <span aria-hidden="true" class={cn("h-2 w-2 shrink-0 rounded-full", live === "live" ? "bg-success" : live === "connecting" ? "animate-pulse bg-primary" : "bg-danger")}></span>
          <span aria-hidden="true" class="truncate">{liveLabel}</span>
        </span>
        {#if needsToken}
          <button
            class="flex h-7 w-7 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
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
