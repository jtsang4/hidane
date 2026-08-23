import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../lib/api.js";
import { fmtDateTime } from "../lib/utils.js";
import {
  notifyPermission,
  requestNotifyPermission,
  type NotifyPermission,
} from "../lib/notify.js";
import { Badge, Button, Card } from "../components/ui/primitives.js";

/** Permission must be requested from a click, so it lives behind a button. */
function NotifyCard() {
  const { t } = useTranslation();
  const [permission, setPermission] = useState<NotifyPermission>(() => notifyPermission());
  return (
    <Card className="space-y-2">
      <div className="flex items-center gap-2">
        <Bell className="h-4 w-4 text-muted" />
        <span className="text-sm font-medium">{t("notify.title")}</span>
      </div>
      <p className="text-xs text-muted">{t("notify.hint")}</p>
      {permission === "granted" && <Badge tone="success">{t("notify.granted")}</Badge>}
      {permission === "denied" && <Badge tone="danger">{t("notify.denied")}</Badge>}
      {permission === "unsupported" && <Badge tone="muted">{t("notify.unsupported")}</Badge>}
      {permission === "default" && (
        <Button
          size="sm"
          onClick={() => void requestNotifyPermission().then(setPermission)}
        >
          {t("notify.enable")}
        </Button>
      )}
    </Card>
  );
}

export function StatusPage() {
  const { t } = useTranslation();
  const { data } = useQuery({
    queryKey: ["status"],
    queryFn: () => api.status(),
    refetchInterval: 10_000,
  });

  if (!data) return <p className="p-6 text-sm text-muted">{t("common.loading")}</p>;

  const heartbeatAge = data.lastHeartbeatAt
    ? Math.round((Date.now() - new Date(data.lastHeartbeatAt).getTime()) / 1000)
    : null;
  const heartbeatOk = heartbeatAge !== null && heartbeatAge < 900;

  const cards: { label: string; value: string; tone?: "success" | "danger" | "muted" }[] = [
    { label: t("status.latestSeq"), value: String(data.latestSeq) },
    {
      label: t("status.triageLag"),
      value: String(data.triageLag),
      tone: data.triageLag < 20 ? "success" : "danger",
    },
    {
      label: t("status.lastHeartbeat"),
      value: data.lastHeartbeatAt
        ? t("status.heartbeatAt", {
            time: fmtDateTime(data.lastHeartbeatAt),
            s: heartbeatAge ?? 0,
          })
        : t("status.none"),
      tone: heartbeatOk ? "success" : "danger",
    },
    { label: t("status.openItems"), value: String(data.openWorkItems) },
    {
      label: t("status.model"),
      value: data.model ?? t("status.none"),
      ...(data.model?.startsWith("error:") ? { tone: "danger" as const } : {}),
    },
  ];

  return (
    <div className="space-y-3 p-4">
      <h1 className="text-lg font-semibold">{t("status.title")}</h1>
      <NotifyCard />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {cards.map((c) => (
          <Card key={c.label}>
            <div className="text-xs text-muted">{c.label}</div>
            <div className="mt-1 flex items-center gap-2 text-lg font-semibold">
              {c.value}
              {c.tone && (
                <Badge tone={c.tone}>
                  {c.tone === "success" ? t("status.ok") : c.tone === "danger" ? t("status.attention") : ""}
                </Badge>
              )}
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}
