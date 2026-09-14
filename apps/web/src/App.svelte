<script lang="ts">
  import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/svelte-query";
  import {
    Activity,
    AlarmClock,
    Brain,
    Flame,
    Languages,
    ListTodo,
    LogOut,
    Logs,
    MessageCircle,
    ScrollText,
  } from "@lucide/svelte";
  import { onMount } from "svelte";
  import i18n, { t, language, switchLanguage } from "./i18n/index.js";
  import {
    ApiError,
    clearToken,
    getToken,
    onUnauthorized,
    setToken,
  } from "./lib/api.js";
  import { resetLiveText } from "./lib/liveText.js";
  import { badgeTitle, completionFrom, notifyPermission } from "./lib/notify.js";
  import { clearToasts, pushToast } from "./lib/toast.js";
  import { navigate, initRouter, routeFor, routerState } from "./lib/router.svelte.js";
  import type { LiveState } from "./lib/live.js";
  import { cn } from "./lib/utils.js";
  import LiveLane from "./components/LiveLane.svelte";
  import Toaster from "./components/Toaster.svelte";
  import Button from "./components/ui/Button.svelte";
  import Card from "./components/ui/Card.svelte";
  import Input from "./components/ui/Input.svelte";
  import ChatPage from "./pages/ChatPage.svelte";
  import EventsPage from "./pages/EventsPage.svelte";
  import ItemDetailPage from "./pages/ItemDetailPage.svelte";
  import ItemsPage from "./pages/ItemsPage.svelte";
  import LogPage from "./pages/LogPage.svelte";
  import MemoryPage from "./pages/MemoryPage.svelte";
  import SchedulesPage from "./pages/SchedulesPage.svelte";
  import StatusPage from "./pages/StatusPage.svelte";

  const BASE_TITLE = "hidane 火种";
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

  const NAV = [
    { to: "/", key: "nav.chat", icon: MessageCircle },
    { to: "/items", key: "nav.items", icon: ListTodo },
    { to: "/events", key: "nav.events", icon: Logs },
    { to: "/log", key: "nav.log", icon: ScrollText },
    { to: "/memory", key: "nav.memory", icon: Brain },
    { to: "/schedules", key: "nav.schedules", icon: AlarmClock },
    { to: "/status", key: "nav.status", icon: Activity },
  ] as const;

  let authed = $state(getToken().length > 0);
  let tokenDraft = $state("");
  let live = $state<LiveState>("connecting");
  let unseen = $state(0);
  let route = $derived(routeFor(routerState.path));
  let nextLang = $derived<"zh" | "en">($language === "en" ? "zh" : "en");
  let liveLabel = $derived($t(live === "live" ? "live.live" : live === "connecting" ? "live.connecting" : "live.offline"));

  onMount(() => {
    const stopRouter = initRouter();
    const stopUnauthorized = onUnauthorized(() => {
      authed = false;
      resetLiveText();
      pushToast(i18n.t("token.invalid"));
    });
    const clear = () => {
      if (document.visibilityState === "visible") unseen = 0;
    };
    const onCompletion = (event: Event) => {
      const done = completionFrom((event as MessageEvent<string>).data);
      if (!done || document.visibilityState === "visible") return;
      unseen += 1;
      if (notifyPermission() === "granted") {
        new Notification(done.ok ? i18n.t("notify.done") : i18n.t("notify.failed"), {
          body: done.summary || done.workItemId || "",
          tag: done.workItemId ?? "hidane",
        });
      }
    };
    document.addEventListener("visibilitychange", clear);
    window.addEventListener("focus", clear);
    window.addEventListener("hidane:event", onCompletion);
    return () => {
      stopRouter();
      stopUnauthorized();
      document.removeEventListener("visibilitychange", clear);
      window.removeEventListener("focus", clear);
      window.removeEventListener("hidane:event", onCompletion);
    };
  });

  $effect(() => {
    document.title = badgeTitle(BASE_TITLE, unseen);
  });

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
    authed = false;
  }

  function linkClick(event: MouseEvent, path: string): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(path);
  }

  function active(path: string): boolean {
    return path === "/" ? route.name === "chat" : routerState.path === path || routerState.path.startsWith(`${path}/`);
  }
</script>

<QueryClientProvider client={queryClient}>
  <LiveLane enabled={authed} onstatechange={(state) => (live = state)} />
  {#if !authed}
    <div class="flex h-full items-center justify-center p-6">
      <Card class="w-full max-w-sm space-y-3">
        <div class="flex items-center gap-2 text-lg font-semibold">
          <Flame size={20} class="text-primary" /> hidane
        </div>
        <p class="text-sm text-muted">{$t("token.prompt")}</p>
        <Input type="password" autofocus bind:value={tokenDraft} placeholder={$t("token.placeholder")} onkeydown={(event) => event.key === "Enter" && submitToken()} />
        <Button class="w-full" onclick={submitToken} disabled={!tokenDraft.trim()}>{$t("token.enter")}</Button>
      </Card>
    </div>
  {:else}
    <div class="flex h-full flex-col-reverse sm:flex-row">
      <nav class="flex shrink-0 justify-around border-t border-border bg-surface p-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] sm:w-44 sm:flex-col sm:justify-start sm:gap-1 sm:border-t-0 sm:border-r sm:p-3 sm:pb-3">
        <div class="hidden items-center gap-2 px-2 pb-3 text-base font-semibold sm:flex">
          <Flame size={20} class="text-primary" /> hidane
        </div>
        {#each NAV as item (item.to)}
          {@const Icon = item.icon}
          <a
            href={item.to}
            class={cn(
              "flex min-w-0 flex-1 items-center justify-center gap-2 rounded-md px-2 py-2 text-sm text-muted hover:bg-surface-2 sm:flex-none sm:justify-start sm:px-3",
              active(item.to) && "bg-surface-2 text-foreground",
            )}
            onclick={(event) => linkClick(event, item.to)}
          >
            <Icon size={16} />
            <span class="hidden sm:inline">{$t(item.key)}</span>
          </a>
        {/each}
        <div class="flex shrink-0 items-center sm:mt-auto sm:block">
          <span class="flex items-center gap-2 px-1 py-2 text-xs text-muted sm:px-3" title={$t("live.hint")} role="status" aria-label={`${$t("live.hint")}: ${liveLabel}`}>
            <span aria-hidden="true" class={cn("h-2 w-2 rounded-full", live === "live" ? "bg-success" : live === "connecting" ? "animate-pulse bg-primary" : "bg-danger")}></span>
            <span aria-hidden="true" class="hidden sm:inline">{liveLabel}</span>
          </span>
        </div>
        <button class="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-md px-2 py-2 text-sm text-muted hover:bg-surface-2 sm:flex-none sm:justify-start sm:px-3" onclick={() => switchLanguage(nextLang)} aria-label={$t("nav.language")}>
          <Languages size={16} />
          <span class="hidden sm:inline">{nextLang === "en" ? $t("nav.english") : $t("nav.chinese")}</span>
        </button>
        <button class="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-md px-2 py-2 text-sm text-muted hover:bg-surface-2 sm:flex-none sm:justify-start sm:px-3" onclick={signOut} aria-label={$t("token.signOut")}>
          <LogOut size={16} />
          <span class="hidden sm:inline">{$t("token.signOut")}</span>
        </button>
      </nav>
      <main class="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {#if route.name === "chat"}
          <ChatPage />
        {:else if route.name === "items"}
          <ItemsPage />
        {:else if route.name === "item"}
          <ItemDetailPage id={route.id} />
        {:else if route.name === "events"}
          <EventsPage />
        {:else if route.name === "log"}
          <LogPage />
        {:else if route.name === "memory"}
          <MemoryPage />
        {:else if route.name === "schedules"}
          <SchedulesPage />
        {:else if route.name === "status"}
          <StatusPage />
        {:else}
          <div class="flex h-full flex-col items-center justify-center gap-3 p-6">
            <p class="text-sm text-muted">{$t("notFound.title")}</p>
            <a href="/" class="text-sm text-primary underline" onclick={(event) => linkClick(event, "/")}>{$t("notFound.back")}</a>
          </div>
        {/if}
      </main>
    </div>
  {/if}
  <Toaster />
</QueryClientProvider>
