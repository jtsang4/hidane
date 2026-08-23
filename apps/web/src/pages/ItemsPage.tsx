import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError } from "../lib/api.js";
import { pushToast } from "../lib/toast.js";
import { runningWorkItems } from "../lib/pending.js";
import { matchesItem } from "../lib/search.js";
import { Time } from "../components/Time.js";
import { Badge, Button, Card, Input, Textarea } from "../components/ui/primitives.js";

/** Opening a work item without first talking to the Primary about it. */
function CreateForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [title, setTitle] = useState("");
  const [brief, setBrief] = useState("");
  const [repo, setRepo] = useState("");

  const create = useMutation({
    mutationFn: () =>
      api.createWorkItem({
        title: title.trim(),
        ...(brief.trim() ? { brief: brief.trim() } : {}),
        ...(repo.trim() ? { repo: repo.trim() } : {}),
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["items"] });
      onDone();
    },
    onError: (err) => pushToast(err instanceof ApiError ? err.message : String(err)),
  });

  return (
    <Card className="space-y-2">
      <Input
        autoFocus
        placeholder={t("items.form.title")}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
      />
      <Textarea
        rows={2}
        placeholder={t("items.form.brief")}
        value={brief}
        onChange={(e) => setBrief(e.target.value)}
      />
      <Input
        placeholder={t("items.form.repo")}
        value={repo}
        onChange={(e) => setRepo(e.target.value)}
      />
      <div className="flex justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onDone}>
          {t("common.cancel")}
        </Button>
        <Button
          size="sm"
          disabled={create.isPending || title.trim().length === 0}
          onClick={() => create.mutate()}
        >
          {t("items.form.create")}
        </Button>
      </div>
    </Card>
  );
}

export function ItemsPage() {
  const { t } = useTranslation();
  const [all, setAll] = useState(false);
  // Items accumulate; scanning a list by eye stops working after a handful.
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
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
        <div className="flex shrink-0 items-center gap-2">
          {query.trim() && (
            <span className="text-xs text-muted">
              {t("items.matched", { n: shown.length, total: items.length })}
            </span>
          )}
          <Button variant="outline" size="sm" onClick={() => setAll((v) => !v)}>
            {all ? t("items.onlyOpen") : t("items.showArchived")}
          </Button>
          {!creating && (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" />
              {t("items.new")}
            </Button>
          )}
        </div>
      </div>
      {creating && <CreateForm onDone={() => setCreating(false)} />}
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
                  {item.id} · {t("items.updated")} <Time iso={item.updatedAt} />
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
