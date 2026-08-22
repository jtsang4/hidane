import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { Flame, ListTodo, MessageCircle, ScrollText, Activity, Logs } from "lucide-react";
import "./styles.css";
import { eventStreamUrl, getToken, setToken } from "./lib/api.js";
import { Button, Card, Input } from "./components/ui/primitives.js";
import { ChatPage } from "./pages/ChatPage.js";
import { ItemsPage } from "./pages/ItemsPage.js";
import { ItemDetailPage } from "./pages/ItemDetailPage.js";
import { EventsPage } from "./pages/EventsPage.js";
import { LogPage } from "./pages/LogPage.js";
import { StatusPage } from "./pages/StatusPage.js";

/** Live lane: one SSE stream drives every view via query invalidation. */
function useEventStream(enabled: boolean) {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!enabled) return;
    const source = new EventSource(eventStreamUrl());
    source.addEventListener("hidane", () => {
      void queryClient.invalidateQueries();
    });
    return () => source.close();
  }, [enabled, queryClient]);
}

function TokenGate({ onDone }: { onDone: () => void }) {
  const [value, setValue] = useState("");
  return (
    <div className="flex h-full items-center justify-center p-6">
      <Card className="w-full max-w-sm space-y-3">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <Flame className="h-5 w-5 text-primary" /> hidane
        </div>
        <p className="text-sm text-muted">输入 API Token（HIDANE_API_TOKEN）。</p>
        <Input
          type="password"
          value={value}
          placeholder="token"
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && value.trim()) {
              setToken(value.trim());
              onDone();
            }
          }}
        />
        <Button
          className="w-full"
          onClick={() => {
            if (value.trim()) {
              setToken(value.trim());
              onDone();
            }
          }}
        >
          进入
        </Button>
      </Card>
    </div>
  );
}

const NAV = [
  { to: "/", label: "会话", icon: MessageCircle },
  { to: "/items", label: "工作项", icon: ListTodo },
  { to: "/events", label: "事件", icon: Logs },
  { to: "/log", label: "日志", icon: ScrollText },
  { to: "/status", label: "状态", icon: Activity },
] as const;

function RootLayout() {
  const [authed, setAuthed] = useState(() => getToken().length > 0);
  useEventStream(authed);

  if (!authed) return <TokenGate onDone={() => setAuthed(true)} />;

  return (
    <div className="flex h-full flex-col-reverse sm:flex-row">
      <nav className="flex shrink-0 justify-around border-t border-border bg-surface p-2 sm:w-44 sm:flex-col sm:justify-start sm:gap-1 sm:border-t-0 sm:border-r sm:p-3">
        <div className="hidden items-center gap-2 px-2 pb-3 text-base font-semibold sm:flex">
          <Flame className="h-5 w-5 text-primary" /> hidane
        </div>
        {NAV.map(({ to, label, icon: Icon }) => (
          <Link
            key={to}
            to={to}
            className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted hover:bg-surface-2 [&.active]:bg-surface-2 [&.active]:text-foreground"
            activeOptions={{ exact: to === "/" }}
          >
            <Icon className="h-4 w-4" />
            <span className="hidden sm:inline">{label}</span>
          </Link>
        ))}
      </nav>
      <main className="min-h-0 flex-1">
        <Outlet />
      </main>
    </div>
  );
}

const rootRoute = createRootRoute({ component: RootLayout });
const routes = [
  createRoute({ getParentRoute: () => rootRoute, path: "/", component: ChatPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/items", component: ItemsPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/items/$id", component: ItemDetailPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/events", component: EventsPage }),
  createRoute({ getParentRoute: () => rootRoute, path: "/log", component: LogPage }),
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
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
