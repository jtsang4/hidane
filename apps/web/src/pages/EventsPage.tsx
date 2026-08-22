import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type HidaneEvent } from "../lib/api.js";
import { fmtDateTime } from "../lib/utils.js";
import { Badge, Card, Input } from "../components/ui/primitives.js";

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

export function EventsPage() {
  const { t } = useTranslation();
  const [kind, setKind] = useState("");
  const [item, setItem] = useState("");
  const { data } = useQuery({
    queryKey: ["events", "browser", kind, item],
    queryFn: () => api.events({ kind, item, tail: 100 }),
  });
  const events = [...(data?.events ?? [])].reverse();

  return (
    <div className="space-y-3 p-4">
      <h1 className="text-lg font-semibold">{t("events.title")}</h1>
      <div className="flex gap-2">
        <Input placeholder={t("events.filterKind")} value={kind} onChange={(e) => setKind(e.target.value)} />
        <Input placeholder={t("events.filterItem")} value={item} onChange={(e) => setItem(e.target.value)} />
      </div>
      <div className="space-y-2">
        {events.map((e) => (
          <EventRow key={e.id} event={e} />
        ))}
        {events.length === 0 && (
          <p className="pt-8 text-center text-sm text-muted">{t("events.empty")}</p>
        )}
      </div>
    </div>
  );
}
