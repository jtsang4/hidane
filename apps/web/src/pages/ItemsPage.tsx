import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { api } from "../lib/api.js";
import { fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card } from "../components/ui/primitives.js";

export function ItemsPage() {
  const [all, setAll] = useState(false);
  const { data } = useQuery({
    queryKey: ["items", all],
    queryFn: () => api.workItems(all),
  });

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">工作项</h1>
        <Button variant="outline" size="sm" onClick={() => setAll((v) => !v)}>
          {all ? "只看进行中" : "包含已完成"}
        </Button>
      </div>
      {(data?.items ?? []).map((item) => (
        <Link key={item.id} to="/items/$id" params={{ id: item.id }} className="block">
          <Card className="transition-colors hover:bg-surface-2">
            <div className="flex items-center justify-between gap-2">
              <div>
                <div className="font-medium">{item.title}</div>
                <div className="mt-1 text-xs text-muted">
                  {item.id} · 更新于 {fmtDateTime(item.updatedAt)}
                </div>
              </div>
              <Badge tone={item.status === "open" ? "success" : "muted"}>{item.status}</Badge>
            </div>
          </Card>
        </Link>
      ))}
      {data?.items.length === 0 && (
        <p className="pt-8 text-center text-sm text-muted">还没有工作项。</p>
      )}
    </div>
  );
}
