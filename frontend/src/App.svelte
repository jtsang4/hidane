<script lang="ts">
  import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/svelte-query";
  import { MessageSquarePlus, PanelLeft, Settings, SquarePen, setLucideProps } from "@lucide/svelte";
  import { onMount } from "svelte";
  import i18n, { t, language } from "./i18n/index.js";
  import { api, ApiError, clearToken, getToken, onUnauthorized, setToken } from "./lib/api.js";
  import { boot } from "./lib/boot.js";
  import { createCommandRunner, formatShortcut, installCommands, isMacPlatform, shortcutKey, type Command } from "./lib/commands.js";
  import { confirmOpen } from "./lib/confirm.svelte.js";
  import { closeMenu, menuOpen, watchContextSelection } from "./lib/contextMenu.svelte.js";
  import type { LiveState } from "./lib/live.js";
  import { resetLiveText } from "./lib/liveText.js";
  import { MAIN_NAV, navTarget } from "./lib/nav.js";
  import { nativeBadge, nativeNotify } from "./lib/native.js";
  import { badgeTitle, completionFrom, notifyPermission } from "./lib/notify.js";
  import {
    conversationHref,
    focusHref,
    initRouter,
    leaveSettings,
    navigate,
    openSettings,
    routeFor,
    routerState,
    SETTINGS_SECTIONS,
    toggleSettings,
  } from "./lib/router.svelte.js";
  import { liveTransport } from "./lib/stream.js";
  import { clearToasts, pushToast } from "./lib/toast.js";
  import { prefs, setSidebarCollapsed, ui } from "./lib/ui.svelte.js";
  import CommandPalette, { type PaletteCommand } from "./components/CommandPalette.svelte";
  import ConfirmHost from "./components/ConfirmHost.svelte";
  import LiveLane from "./components/LiveLane.svelte";
  import MenuHost from "./components/MenuHost.svelte";
  import NewTaskDialog from "./components/NewTaskDialog.svelte";
  import PhoneNav from "./components/PhoneNav.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import Toaster from "./components/Toaster.svelte";
  import Button from "./components/ui/Button.svelte";
  import BrandMark from "./components/BrandMark.svelte";
  import Input from "./components/ui/Input.svelte";
  import ConversationPage from "./pages/ConversationPage.svelte";
  import ItemsPage from "./pages/ItemsPage.svelte";
  import LogPage from "./pages/LogPage.svelte";
  import MemoryPage from "./pages/MemoryPage.svelte";
  import SchedulesPage from "./pages/SchedulesPage.svelte";
  import SettingsView from "./pages/SettingsView.svelte";

  const queryClient = new QueryClient({
    defaultOptions: { queries: { staleTime: 3_000, retry: 1 } },
    queryCache: new QueryCache({
      onError: (error) => {
        if (error instanceof ApiError && error.status === 401) return;
        pushToast(
          error instanceof ApiError && error.status === 0
            ? i18n.t("error.offline")
            : i18n.t("error.request", { message: error instanceof Error ? error.message : String(error) }),
        );
      },
    }),
  });

  // Lucide's 2px stroke draws heavier than 13px text; a finer line matches the type.
  setLucideProps({ strokeWidth: 1.5 });

  const desktop = boot().desktop;
  const mac = isMacPlatform();
  // The desktop webview is the only client of its backend: there is no token to ask for.
  const needsToken = boot().auth;
  let authed = $state(!needsToken || getToken().length > 0);
  let tokenDraft = $state("");
  let live = $state<LiveState>("connecting");
  let readyReported = false;

  // The desktop shell learns the page loaded and its live channel works.
  function onLiveState(state: LiveState) {
    live = state;
    if (state === "live" && desktop && !readyReported) {
      readyReported = true;
      void api.uiReady(liveTransport()).catch(() => undefined);
    }
  }

  let unseen = $state(0);
  /** The Dock badge last sent, so a reload does not re-send zeros. */
  let badgeSent = 0;
  let route = $derived(routeFor(routerState.path));

  const keyHint = (command: Command) => {
    const key = shortcutKey(command);
    return key ? formatShortcut(key, mac) : "";
  };

  /** What a command does; the runner in front of it drops a second delivery of the same key press. */
  function execute(command: Command): void {
    if (!authed || confirmOpen()) return;
    if (menuOpen()) closeMenu();
    // A form is open: its fields keep the keys until it is closed.
    if (ui.newTaskOpen) return;
    if (command === "search") {
      ui.paletteOpen = !ui.paletteOpen;
      return;
    }
    ui.paletteOpen = false;
    if (command === "open-settings") toggleSettings();
    else if (command === "new-task") ui.newTaskOpen = true;
    else if (command === "toggle-sidebar") setSidebarCollapsed(!ui.sidebarCollapsed);
    else if (command === "focus-composer") {
      if (route.name !== "chat") navigate(routerState.returnTo.startsWith("/?") ? routerState.returnTo : "/");
      ui.composerFocus = true;
    } else {
      const to = navTarget(command);
      if (to && !(to === "/" && route.name === "chat")) navigate(to);
    }
  }
  const runCommand = createCommandRunner(execute);

  let paletteCommands = $derived.by((): PaletteCommand[] => {
    void $language;
    const en = i18n.getFixedT("en");
    const zh = i18n.getFixedT("zh");
    return [
      ...MAIN_NAV.map((item) => ({
        id: item.command,
        group: "go" as const,
        label: i18n.t("palette.goTo", { page: i18n.t(item.key) }),
        keywords: [en(item.key), zh(item.key), item.to],
        shortcut: keyHint(item.command),
        icon: item.icon,
        run: () => execute(item.command),
      })),
      {
        id: "new-task",
        group: "action",
        label: i18n.t("shell.newTask"),
        keywords: [en("shell.newTask"), zh("shell.newTask")],
        shortcut: keyHint("new-task"),
        icon: SquarePen,
        run: () => execute("new-task"),
      },
      {
        id: "focus-composer",
        group: "action",
        label: i18n.t("palette.focusComposer"),
        keywords: [en("palette.focusComposer"), zh("palette.focusComposer")],
        shortcut: keyHint("focus-composer"),
        icon: MessageSquarePlus,
        run: () => execute("focus-composer"),
      },
      {
        id: "toggle-sidebar",
        group: "action",
        label: i18n.t("palette.toggleSidebar"),
        keywords: [en("palette.toggleSidebar"), zh("palette.toggleSidebar")],
        shortcut: keyHint("toggle-sidebar"),
        icon: PanelLeft,
        run: () => execute("toggle-sidebar"),
      },
      ...SETTINGS_SECTIONS.map((section) => ({
        id: `settings:${section}`,
        group: "settings" as const,
        label: i18n.t("palette.openSettings", { section: i18n.t(`settings.sections.${section}`) }),
        keywords: [en(`settings.sections.${section}`), zh(`settings.sections.${section}`), en("shell.settings"), zh("shell.settings")],
        shortcut: section === "general" ? keyHint("open-settings") : "",
        icon: Settings,
        run: () => openSettings(section),
      })),
    ];
  });

  onMount(() => {
    const stopRouter = initRouter();
    const stopCommands = installCommands(runCommand);
    const stopSelectionWatch = watchContextSelection();
    window.addEventListener("hidane:event", onCompletion);
    const stopUnauthorized = onUnauthorized(() => {
      if (!needsToken) return;
      authed = false;
      resetLiveText();
      pushToast(i18n.t("token.invalid"));
    });
    return () => {
      window.removeEventListener("hidane:event", onCompletion);
      stopRouter();
      stopCommands();
      stopSelectionWatch();
      stopUnauthorized();
    };
  });

  /** In front: the window has focus and the page is shown. */
  const inFront = () => document.visibilityState === "visible" && document.hasFocus();

  function cameBack(): void {
    if (inFront()) unseen = 0;
  }

  function onCompletion(event: Event): void {
    const done = completionFrom((event as MessageEvent<string>).data);
    if (!done || inFront()) return;
    unseen += 1;
    if (!prefs.notify) return;
    const title = done.ok ? i18n.t("notify.done") : i18n.t("notify.failed");
    const body = done.summary || done.workItemId || "";
    if (desktop) nativeNotify(title, body);
    else if (notifyPermission() === "granted") new Notification(title, { body, tag: done.workItemId ?? "hidane" });
  }

  $effect(() => {
    const count = prefs.badge ? unseen : 0;
    if (desktop) {
      if (count !== badgeSent) {
        badgeSent = count;
        nativeBadge(count);
      }
    } else {
      document.title = badgeTitle($t("common.appName"), count);
    }
  });

  /** Esc leaves settings once nothing on top of it wants the key; in a field it first lets go of the field. */
  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== "Escape" || event.defaultPrevented || event.isComposing) return;
    if (ui.paletteOpen || ui.newTaskOpen || confirmOpen() || menuOpen() || route.name !== "settings") return;
    const target = event.target;
    if (target instanceof HTMLElement && target.matches("input, textarea, select, [contenteditable='true']")) {
      target.blur();
      return;
    }
    leaveSettings();
  }

  /**
   * The webview's own menu (Reload, Inspect…) has no place in an app. Fields
   * and selected text keep it for cut/copy/paste; menus of our own have
   * already taken the event.
   */
  function onContextMenu(event: MouseEvent): void {
    if (!desktop || event.defaultPrevented) return;
    const target = event.target;
    if (target instanceof Element && target.closest("input, textarea, [contenteditable='true']")) return;
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed && selection.toString().trim()) return;
    event.preventDefault();
  }

  /** A file dropped outside the composer must not replace the app with the file. */
  function onWindowDrop(event: DragEvent): void {
    if (event.dataTransfer?.types.includes("Files")) event.preventDefault();
  }

  function submitToken(): void {
    const value = tokenDraft.trim();
    if (!value) return;
    setToken(value);
    clearToasts();
    tokenDraft = "";
    authed = true;
  }

  function signOut(): void {
    clearToken();
    clearToasts();
    // Signing out does not reload, so an in-flight reply would otherwise still
    // be held in memory and shown to whoever signs in next on this tab.
    resetLiveText();
    ui.paletteOpen = false;
    ui.newTaskOpen = false;
    authed = false;
  }
</script>

<svelte:window
  onfocus={cameBack}
  onkeydown={onKeydown}
  ondragover={onWindowDrop}
  ondrop={onWindowDrop}
/>
<svelte:document onvisibilitychange={cameBack} oncontextmenu={onContextMenu} />

<QueryClientProvider client={queryClient}>
  <LiveLane enabled={authed} onstatechange={onLiveState} />
  {#if !authed}
    <div class="drag-region relative flex h-full items-center justify-center overflow-hidden p-6">
      <!-- The ember: one warm light in a dark room, behind the only thing to do here. -->
      <div class="pointer-events-none absolute inset-0 ember-light" aria-hidden="true"></div>
      <div class="grain pointer-events-none absolute inset-0" aria-hidden="true"></div>
      <div class="relative w-full max-w-[340px] animate-dialog-in">
        <div class="mb-6 flex flex-col items-center text-center">
          <div class="relative mb-4" aria-hidden="true">
            <div class="absolute -inset-4 rounded-full bg-primary/25 blur-xl"></div>
            <div class="relative grid size-12 place-items-center rounded-2xl border border-border bg-linear-to-b from-surface-2 to-surface shadow-popover">
              <BrandMark size={24} />
            </div>
          </div>
          <h1 class="text-xl font-semibold tracking-tight">Hidane</h1>
          <p class="mt-1.5 text-sm text-muted">{$t("token.prompt")}</p>
        </div>
        <div class="space-y-2 rounded-xl border border-border bg-surface/80 p-3 shadow-dialog backdrop-blur">
          <Input type="password" class="h-8" autofocus bind:value={tokenDraft} placeholder={$t("token.placeholder")} onkeydown={(event) => event.key === "Enter" && submitToken()} />
          <Button size="lg" class="w-full" onclick={submitToken} disabled={!tokenDraft.trim()}>{$t("token.enter")}</Button>
        </div>
      </div>
    </div>
  {:else if route.name === "settings"}
    <SettingsView section={route.section} onsignout={signOut} />
  {:else if route.name !== "redirect" && route.name !== "item"}
    <div class="flex h-full">
      {#if !ui.sidebarCollapsed}
        <div class="hidden h-full sm:flex"><Sidebar {live} onsignout={signOut} /></div>
      {/if}
      <div class="flex min-w-0 flex-1 flex-col">
        <main class="min-h-0 flex-1">
          {#if route.name === "chat"}
            <ConversationPage />
          {:else if route.name === "items"}
            <ItemsPage />
          {:else if route.name === "log"}
            <LogPage />
          {:else if route.name === "memory"}
            <MemoryPage />
          {:else if route.name === "schedules"}
            <SchedulesPage />
          {:else}
            <div class="flex h-full flex-col">
              <div class="drag-region h-[52px] shrink-0 border-b border-border"></div>
              <div class="flex flex-1 flex-col items-center justify-center gap-3 p-6">
                <p class="text-sm text-muted">{$t("notFound.title")}</p>
                <Button variant="secondary" onclick={() => navigate(conversationHref({}))}>{$t("notFound.back")}</Button>
              </div>
            </div>
          {/if}
        </main>
        <PhoneNav />
      </div>
    </div>
  {/if}
  {#if authed && ui.paletteOpen}
    <CommandPalette
      commands={paletteCommands}
      onclose={() => (ui.paletteOpen = false)}
      onopenitem={(id) => navigate(focusHref(id))}
      onopenmessage={(id) => navigate(conversationHref({ at: id }))}
    />
  {/if}
  {#if authed && ui.newTaskOpen}
    <NewTaskDialog onclose={() => (ui.newTaskOpen = false)} />
  {/if}
  <ConfirmHost />
  <MenuHost />
  <Toaster />
</QueryClientProvider>
