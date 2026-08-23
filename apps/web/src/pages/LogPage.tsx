import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Markdown from "react-markdown";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api.js";
import { shiftDay, today } from "../lib/utils.js";
import { Button, Input } from "../components/ui/primitives.js";

export function LogPage() {
  const { t } = useTranslation();
  const [day, setDay] = useState(today());
  const valid = /^\d{4}-\d{2}-\d{2}$/.test(day);
  const { data, isLoading } = useQuery({
    queryKey: ["worklog", day],
    queryFn: () => api.worklog(day),
    enabled: valid,
  });

  // A worklog is read by walking days, not by typing dates.
  const isToday = day === today();
  const markdown = data?.markdown ?? "";
  // A day with nothing in it still renders a heading, so an "is the markdown
  // empty" check would never fire — the API reports the count instead.
  const empty = data !== undefined && data.eventCount === 0;

  return (
    <div className="space-y-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="text-lg font-semibold">{t("log.title")}</h1>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="icon"
            aria-label={t("log.prev")}
            onClick={() => setDay((d) => shiftDay(d, -1))}
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Input
            type="date"
            className="w-40"
            value={day}
            onChange={(e) => setDay(e.target.value)}
          />
          <Button
            variant="outline"
            size="icon"
            aria-label={t("log.next")}
            disabled={isToday}
            onClick={() => setDay((d) => shiftDay(d, 1))}
          >
            <ChevronRight className="h-4 w-4" />
          </Button>
          <Button variant="outline" size="sm" disabled={isToday} onClick={() => setDay(today())}>
            {t("log.today")}
          </Button>
        </div>
      </div>
      {isLoading && <p className="text-sm text-muted">{t("common.loading")}</p>}
      {empty && <p className="pt-8 text-center text-sm text-muted">{t("log.empty")}</p>}
      <article className="prose-sm max-w-none space-y-2 [&_h1]:text-xl [&_h1]:font-bold [&_h2]:mt-4 [&_h2]:text-base [&_h2]:font-semibold [&_li]:ml-4 [&_li]:list-disc [&_code]:rounded [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:text-xs">
        <Markdown>{markdown}</Markdown>
      </article>
    </div>
  );
}
