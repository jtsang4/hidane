import { StrictMode, useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import {
  QueryCache,
  QueryClient,
  QueryClientProvider,
  useQueryClient,
} from "@tanstack/react-query";
import {
  Flame,
  ListTodo,
  MessageCircle,
  ScrollText,
  Activity,
  Logs,
  Languages,
  Brain,
  AlarmClock,
  LogOut,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import "./styles.css";
import i18n, { switchLanguage } from "./i18n/index.js";
import {
  ApiError,
  clearToken,
  eventStreamUrl,
  getToken,
  onUnauthorized,
  setToken,
} from "./lib/api.js";
import { livenessFrom, shouldReconnect, type LiveState } from "./lib/live.js";
import { badgeTitle, completionFrom, notifyPermission } from "./lib/notify.js";
import { clearToasts, pushToast } from "./lib/toast.js";
import { cn } from "./lib/utils.js";
import { Button, Card, Input } from "./components/ui/primitives.js";
import { Toaster } from "./components/Toaster.js";
import { ChatPage } from "./pages/ChatPage.js";
import { ItemsPage } from "./pages/ItemsPage.js";
import { ItemDetailPage } from "./pages/ItemDetailPage.js";
import { EventsPage } from "./pages/EventsPage.js";
import { LogPage } from "./pages/LogPage.js";
import { MemoryPage } from "./pages/MemoryPage.js";
import { SchedulesPage } from "./pages/SchedulesPage.js";
import { StatusPage } from "./pages/StatusPage.js";

/**
 * Live lane: one SSE stream drives every view via query invalidation.
 * Its state is surfaced because a silently-dead stream leaves every view
 * showing stale data that looks perfectly current — and that death really is
 * silent: a killed server leaves EventSource at readyState OPEN with no error,
 * so liveness is decided by the absence of the server's keep-alive ping.
 */
const BASE_TITLE = "hidane \u706b\u7a2e";

/**
 * Announce finished work. A run takes minutes, so the moment it ends is
 * usually the moment nobody is looking: the tab title carries an unread count,
 * and (once allowed) a desktop notification names what finished.
 */
function useCompletionAlerts(): void {
  const [unseen, setUnseen] = useState(0);

  useEffect(() => {
    document.title = badgeTitle(BASE_TITLE, unseen);
  }, [unseen]);

  useEffect(() => {
    const clear = () => {
      if (document.visibilityState === "visible") setUnseen(0);
    };
    document.addEventListener("visibilitychange", clear);
    window.addEventListener("focus", clear);
    return () => {
      document.removeEventListener("visibilitychange", clear);
      window.removeEventListener("focus", clear);
    };
  }, []);

  useEffect(() => {
    const onCompletion = (e: Event) => {
      const done = completionFrom((e as MessageEvent<string>).data);
      // Only while away: interrupting someone who is already watching is noise.
      if (!done || document.visibilityState === "visible") return;
      setUnseen((n) => n + 1);
      if (notifyPermission() === "granted") {
        new Notification(done.ok ? i18n.t("notify.done") : i18n.t("notify.failed"), {
          body: done.summary || done.workItemId || "",
          tag: done.workItemId ?? "hidane",
        });
      }
    };
    window.addEventListener("hidane:event", onCompletion);
    return () => window.removeEventListener("hidane:event", onCompletion);
  }, []);
}

function useEventStream(enabled: boolean): LiveState {
  const queryClient = useQueryClient();
  const [state, setState] = useState<LiveState>("connecting");
  const [attempt, setAttempt] = useState(0);
  // These span reconnect attempts on purpose — see livenessFrom.
  const lastHeardAt = useRef(Date.now());
  const everHeard = useRef(false);

  useEffect(() => {
    if (!enabled) return;
    const connectedAt = Date.now();
    const source = new EventSource(eventStreamUrl());
    const heard = () => {
      lastHeardAt.current = Date.now();
      everHeard.current = true;
      setState("live");
    };
    source.addEventListener("hello", heard);
    source.addEventListener("ping", heard);
    source.addEventListener("hidane", (e) => {
      heard();
      // Re-broadcast so alerting stays independent of the transport.
      window.dispatchEvent(
        new MessageEvent("hidane:event", { data: (e as MessageEvent<string>).data }),
      );
      void queryClient.invalidateQueries();
    });
    const timer = setInterval(() => {
      const now = Date.now();
      setState(livenessFrom(lastHeardAt.current, everHeard.current, now));
      if (shouldReconnect(lastHeardAt.current, connectedAt, now)) {
        setAttempt((n) => n + 1);
      }
    }, 5_000);
    return () => {
      clearInterval(timer);
      source.close();
    };
  }, [enabled, queryClient, attempt]);

  return state;
}

function TokenGate({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const [value, setValue] = useState("");
  const submit = () => {
    if (!value.trim()) return;
    setToken(value.trim());
    clearToasts();
    onDone();
  };
  return (
    <div className="flex h-full items-center justify-center p-6">
      <Card className="w-full max-w-sm space-y-3">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <Flame className="h-5 w-5 text-primary" /> hidane
        </div>
        <p className="text-sm text-muted">{t("token.prompt")}</p>
        <Input
          type="password"
          autoFocus
          value={value}
          placeholder={t("token.placeholder")}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        <Button className="w-full" onClick={submit} disabled={!value.trim()}>
          {t("token.enter")}
        </Button>
      </Card>
      <Toaster />
    </div>
  );
}

const NAV = [
  { to: "/", key: "nav.chat", icon: MessageCircle },
  { to: "/items", key: "nav.items", icon: ListTodo },
  { to: "/events", key: "nav.events", icon: Logs },
  { to: "/log", key: "nav.log", icon: ScrollText },
  { to: "/memory", key: "nav.memory", icon: Brain },
  { to: "/schedules", key: "nav.schedules", icon: AlarmClock },
  { to: "/status", key: "nav.status", icon: Activity },
] as const;

function LiveDot({ state }: { state: LiveState }) {
  const { t } = useTranslation();
  const label =
    state === "live"
      ? t("live.live")
      : state === "connecting"
        ? t("live.connecting")
        : t("live.offline");
  return (
    <span
      className="flex items-center gap-2 px-1 py-2 text-xs text-muted sm:px-3"
      title={t("live.hint")}
      // The label is announced once from here; the text node below is visual
      // only, and on narrow screens it is not rendered at all.
      role="status"
      aria-label={`${t("live.hint")}: ${label}`}
    >
      <span
        aria-hidden="true"
        className={cn(
          "h-2 w-2 rounded-full",
          state === "live"
            ? "bg-success"
            : state === "connecting"
              ? "animate-pulse bg-primary"
              : "bg-danger",
        )}
      />
      <span aria-hidden="true" className="hidden sm:inline">
        {label}
      </span>
    </span>
  );
}

function RootLayout() {
  const { t } = useTranslation();
  const [authed, setAuthed] = useState(() => getToken().length > 0);
  const live = useEventStream(authed);
  useCompletionAlerts();

  // A rejected token drops the app back to the prompt instead of stranding it.
  useEffect(
    () =>
      onUnauthorized(() => {
        setAuthed(false);
        pushToast(i18n.t("token.invalid"));
      }),
    [],
  );

  const signOut = useCallback(() => {
    clearToken();
    clearToasts();
    setAuthed(false);
  }, []);

  if (!authed) return <TokenGate onDone={() => setAuthed(true)} />;

  const nextLang = i18n.language === "en" ? "zh" : "en";
  return (
    <div className="flex h-full flex-col-reverse sm:flex-row">
      {/* pb-safe: the bar sits at the very bottom on a phone, where the home
          indicator would otherwise overlap the last row of tap targets. */}
      <nav className="flex shrink-0 justify-around border-t border-border bg-surface p-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] sm:w-44 sm:flex-col sm:justify-start sm:gap-1 sm:border-t-0 sm:border-r sm:p-3 sm:pb-3">
        <div className="hidden items-center gap-2 px-2 pb-3 text-base font-semibold sm:flex">
          <Flame className="h-5 w-5 text-primary" /> hidane
        </div>
        {NAV.map(({ to, key, icon: Icon }) => (
          <Link
            key={to}
            to={to}
            className="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-md px-2 py-2 text-sm text-muted hover:bg-surface-2 sm:flex-none sm:justify-start sm:px-3 [&.active]:bg-surface-2 [&.active]:text-foreground"
            activeOptions={{ exact: to === "/" }}
          >
            <Icon className="h-4 w-4" />
            <span className="hidden sm:inline">{t(key)}</span>
          </Link>
        ))}
        <div className="flex shrink-0 items-center sm:mt-auto sm:block">
          <LiveDot state={live} />
        </div>
        <button
          className="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-md px-2 py-2 text-sm text-muted hover:bg-surface-2 sm:flex-none sm:justify-start sm:px-3"
          onClick={() => switchLanguage(nextLang)}
          aria-label="switch language"
        >
          <Languages className="h-4 w-4" />
          <span className="hidden sm:inline">{nextLang === "en" ? "English" : "中文"}</span>
        </button>
        <button
          className="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-md px-2 py-2 text-sm text-muted hover:bg-surface-2 sm:flex-none sm:justify-start sm:px-3"
          onClick={signOut}
          aria-label={t("token.signOut")}
        >
          <LogOut className="h-4 w-4" />
          <span className="hidden sm:inline">{t("token.signOut")}</span>
        </button>
      </nav>
      <main className="min-h-0 flex-1">
        <Outlet />
      </main>
      <Toaster />
    </div>
  );
}

function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 p-6">
      <p className="text-sm text-muted">{t("notFound.title")}</p>
      <Link to="/" className="text-sm text-primary underline">
        {t("notFound.back")}
      </Link>
    </div>
  );
}

const rootRoute = createRootRoute({ component: RootLayout, notFoundComponent: NotFound });
const routes = [
  createRoute({ getParentRoute: () => rootRoute, path: "/", component: ChatPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/items", component: ItemsPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/items/$id", component: ItemDetailPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/events", component: EventsPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/log", component: LogPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/memory", component: MemoryPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/schedules", component: SchedulesPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/status", component: StatusPage }),
];

const router = createRouter({ routeTree: rootRoute.addChildren(routes) });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 3_000, retry: 1 } },
  // 401 is handled by the gate; every other failure must be visible rather than
  // rendering an empty page that reads as "nothing here yet".
  queryCache: new QueryCache({
    onError: (error) => {
      if (error instanceof ApiError && error.status === 401) return;
      pushToast(
        error instanceof ApiError && error.status === 0
          ? i18n.t("error.offline")
          : `${i18n.t("error.title")}: ${error.message}`,
      );
    },
  }),
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
