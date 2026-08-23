import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api.js";
import { runningWorkItems } from "../lib/pending.js";
import { fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card } from "../components/ui/primitives.js";

export function ItemsPage() {
  const { t } = useTranslation();
  const [all, setAll] = useState(false);
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

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">{t("items.title")}</h1>
        <Button variant="outline" size="sm" onClick={() => setAll((v) => !v)}>
          {all ? t("items.onlyOpen") : t("items.includeAll")}
        </Button>
      </div>
      {(data?.items ?? []).map((item) => (
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
      {data?.items.length === 0 && (
        <p className="pt-8 text-center text-sm text-muted">{t("items.empty")}</p>
      )}
    </div>
  );
}
