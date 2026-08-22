import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api.js";
import { fmtDateTime } from "../lib/utils.js";
import { Badge, Card } from "../components/ui/primitives.js";

export function StatusPage() {
  const { data } = useQuery({
    queryKey: ["status"],
    queryFn: () => api.status(),
    refetchInterval: 10_000,
  });

  if (!data) return <p className="p-6 text-sm text-muted">加载中…</p>;

  const heartbeatAge = data.lastHeartbeatAt
    ? Math.round((Date.now() - new Date(data.lastHeartbeatAt).getTime()) / 1000)
    : null;
  const heartbeatOk = heartbeatAge !== null && heartbeatAge < 900;

  const cards: { label: string; value: string; tone?: "success" | "danger" | "muted" }[] = [
    { label: "最新事件 seq", value: String(data.latestSeq) },
    {
      label: "分诊游标滞后",
      value: String(data.triageLag),
      tone: data.triageLag < 20 ? "success" : "danger",
    },
    {
      label: "上次心跳",
      value: data.lastHeartbeatAt
        ? `${fmtDateTime(data.lastHeartbeatAt)}（${heartbeatAge}s 前）`
        : "无",
      tone: heartbeatOk ? "success" : "danger",
    },
    { label: "进行中工作项", value: String(data.openWorkItems) },
  ];

  return (
    <div className="space-y-3 p-4">
      <h1 className="text-lg font-semibold">运行状态</h1>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {cards.map((c) => (
          <Card key={c.label}>
            <div className="text-xs text-muted">{c.label}</div>
            <div className="mt-1 flex items-center gap-2 text-lg font-semibold">
              {c.value}
              {c.tone && <Badge tone={c.tone}>{c.tone === "success" ? "正常" : c.tone === "danger" ? "注意" : ""}</Badge>}
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}
