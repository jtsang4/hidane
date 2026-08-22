import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type HidaneEvent } from "../lib/api.js";
import { fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card, Input } from "../components/ui/primitives.js";

const PAGE_SIZE = 50;

function EventRow({ event }: { event: HidaneEvent }) {
  const [open, setOpen] = useState(false);
  return (
    <Card className="cursor-pointer p-3 text-sm" onClick={() => setOpen((v) => !v)}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs text-muted">#{event.seq}</span>
        <Badge tone={event.source.startsWith("agent:") ? "default" : "muted"}>
          {event.kind}
        </Badge>
        <span className="text-xs text-muted">{event.source}</span>
        {event.workItemId && (
          <span className="font-mono text-xs text-muted">{event.workItemId}</span>
        )}
        <span className="ml-auto text-xs text-muted">{fmtDateTime(event.ts)}</span>
      </div>
      {open && (
        <pre className="mt-2 overflow-x-auto rounded bg-background p-2 text-xs">
          {JSON.stringify(event, null, 2)}
        </pre>
      )}
    </Card>
  );
}

/**
 * Cursor-paginated log browser: the newest page is live (SSE-driven refetch),
 * older pages are appended on demand and never re-fetched, so the DOM grows
 * only when the user asks for more.
 */
export function EventsPage() {
  const { t } = useTranslation();
  const [kind, setKind] = useState("");
  const [item, setItem] = useState("");
  const [olderPages, setOlderPages] = useState<HidaneEvent[][]>([]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [exhausted, setExhausted] = useState(false);
  const seenNewestSeq = useRef<number | null>(null);

  // Reset accumulated pages whenever the filter changes.
  useEffect(() => {
    setOlderPages([]);
    setExhausted(false);
    seenNewestSeq.current = null;
  }, [kind, item]);

  const { data } = useQuery({
    queryKey: ["events", "page", kind, item],
    queryFn: () => api.eventsPage({ kind, item, limit: PAGE_SIZE }),
  });

  // API returns ascending seq; newest-first display means reversing each page,
  // and older pages append below (each older page is itself newest-first).
  const newest = useMemo(() => [...(data?.events ?? [])].reverse(), [data?.events]);
  const older = useMemo(
    () => olderPages.flatMap((page) => [...page].reverse()),
    [olderPages],
  );
  const rows = [...newest, ...older];
  const hasMore = (data?.hasMore ?? false) && !exhausted;

  const loadMore = useCallback(async () => {
    // Rows are newest-first, so the oldest loaded event is the LAST row —
    // and within the raw ascending pages it is the first element.
    const oldestLoaded =
      olderPages.at(-1)?.[0]?.seq ?? data?.events[0]?.seq ?? data?.oldestSeq ?? undefined;
    if (oldestLoaded === undefined || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await api.eventsPage({
        kind,
        item,
        before: oldestLoaded,
        limit: PAGE_SIZE,
      });
      if (page.events.length > 0) setOlderPages((pages) => [...pages, page.events]);
      if (!page.hasMore || page.events.length === 0) setExhausted(true);
    } finally {
      setLoadingMore(false);
    }
  }, [olderPages, data?.events, data?.oldestSeq, kind, item, loadingMore]);

  const newestSeq = newest[0]?.seq ?? null;
  const freshCount =
    seenNewestSeq.current !== null && newestSeq !== null && newestSeq > seenNewestSeq.current
      ? newest.filter((e) => e.seq > (seenNewestSeq.current as number)).length
      : 0;
  useEffect(() => {
    if (newestSeq !== null && seenNewestSeq.current === null) {
      seenNewestSeq.current = newestSeq;
    }
  }, [newestSeq]);

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">{t("events.title")}</h1>
        <span className="text-xs text-muted">
          {t("events.showing", { n: rows.length })}
          {freshCount > 0 ? ` · ${t("events.fresh", { n: freshCount })}` : ""}
        </span>
      </div>
      <div className="flex gap-2">
        <Input
          placeholder={t("events.filterKind")}
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        />
        <Input
          placeholder={t("events.filterItem")}
          value={item}
          onChange={(e) => setItem(e.target.value)}
        />
      </div>
      <div className="space-y-2">
        {rows.map((e) => (
          <EventRow key={e.id} event={e} />
        ))}
        {rows.length === 0 && (
          <p className="pt-8 text-center text-sm text-muted">{t("events.empty")}</p>
        )}
      </div>
      {rows.length > 0 && (
        <div className="pt-2 text-center">
          {hasMore ? (
            <Button variant="outline" size="sm" onClick={() => void loadMore()} disabled={loadingMore}>
              {loadingMore ? t("common.loading") : t("events.loadMore")}
            </Button>
          ) : (
            <span className="text-xs text-muted">{t("events.allLoaded")}</span>
          )}
        </div>
      )}
    </div>
  );
}
