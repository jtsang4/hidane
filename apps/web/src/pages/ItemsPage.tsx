import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api.js";
import { runningWorkItems } from "../lib/pending.js";
import { matchesItem } from "../lib/search.js";
import { fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card, Input } from "../components/ui/primitives.js";

export function ItemsPage() {
  const { t } = useTranslation();
  const [all, setAll] = useState(false);
  // Items accumulate; scanning a list by eye stops working after a handful.
  const [query, setQuery] = useState("");
  const { data } = useQuery({
    queryKey: ["items", all],
    queryFn: () => api.workItems(all),
  });
  // Which item is busy right now is the first thing you want from a list of
  // them, and the status column cannot say it: "open" covers idle and running.
  const { data: recent } = useQuery({
    queryKey: ["events", "executions"],
    queryFn: () => api.events({ tail: 300 }),
  });
  const running = runningWorkItems(recent?.events ?? []);
  const items = data?.items ?? [];
  const shown = query.trim() ? items.filter((i) => matchesItem(i, query)) : items;

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">{t("items.title")}</h1>
        <div className="flex items-center gap-2">
          {query.trim() && (
            <span className="text-xs text-muted">
              {t("items.matched", { n: shown.length, total: items.length })}
            </span>
          )}
          <Button variant="outline" size="sm" onClick={() => setAll((v) => !v)}>
            {all ? t("items.onlyOpen") : t("items.includeAll")}
          </Button>
        </div>
      </div>
      <Input
        placeholder={t("items.search")}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {shown.map((item) => (
        <Link key={item.id} to="/items/$id" params={{ id: item.id }} className="block">
          <Card className="transition-colors hover:bg-surface-2">
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0">
                <div className="font-medium">{item.title}</div>
                <div className="mt-1 text-xs text-muted">
                  {item.id} · {t("items.updatedAt", { time: fmtDateTime(item.updatedAt) })}
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {running.has(item.id) && (
                  <Badge tone="default">
                    <span className="mr-1 inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-primary" />
                    {t("items.working")}
                  </Badge>
                )}
                <Badge tone={item.status === "open" ? "success" : "muted"}>{item.status}</Badge>
              </div>
            </div>
          </Card>
        </Link>
      ))}
      {items.length === 0 && (
        <p className="pt-8 text-center text-sm text-muted">{t("items.empty")}</p>
      )}
      {items.length > 0 && shown.length === 0 && (
        <p className="pt-8 text-center text-sm text-muted">{t("items.noMatch")}</p>
      )}
    </div>
  );
}
