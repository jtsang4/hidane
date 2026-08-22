import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, SendHorizonal } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, type HidaneEvent } from "../lib/api.js";
import { conversationEvents, executionGroups, payloadText, type ExecutionGroup } from "../lib/grouping.js";
import { cn, fmtTime, fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card, Textarea } from "../components/ui/primitives.js";

function Execution({ group }: { group: ExecutionGroup }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const tone = group.ok === null ? "muted" : group.ok ? "success" : "danger";
  const label =
    group.ok === null ? t("item.running") : group.ok ? t("item.success") : t("item.failed");
  return (
    <Card className="p-3">
      <button
        className="flex w-full items-center gap-2 text-left text-sm"
        onClick={() => setOpen((v) => !v)}
      >
        {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        <span className="font-mono text-xs">{group.executionId}</span>
        <Badge tone={tone}>{label}</Badge>
        <span className="ml-auto text-xs text-muted">
          {group.started ? fmtTime(group.started.ts) : ""}
          {group.sideEffects.length > 0 &&
            ` · ${t("item.toolCalls", { n: (group.sideEffects.length / 2) | 0 })}`}
        </span>
      </button>
      {open && (
        <div className="mt-2 space-y-1 border-t border-border pt-2 text-xs">
          {group.started && (
            <p className="whitespace-pre-wrap text-muted">
              {t("item.instructions", {
                text: String(group.started.payload["instructions"] ?? ""),
              })}
            </p>
          )}
          {group.sideEffects.map((e: HidaneEvent) => (
            <div key={e.id} className="flex gap-2 font-mono">
              <span className={cn(e.kind.endsWith("intent") ? "text-primary" : "text-success")}>
                {e.kind.endsWith("intent") ? "→" : "←"}
              </span>
              <span>{String(e.payload["tool"] ?? "")}</span>
              <span className="truncate text-muted">
                {e.kind.endsWith("intent")
                  ? String(e.payload["input"] ?? "").slice(0, 120)
                  : e.payload["isError"] === true
                    ? "error"
                    : "ok"}
              </span>
            </div>
          ))}
          {group.finished && (
            <p className="whitespace-pre-wrap pt-1">
              {String(group.finished.payload["summary"] ?? "").slice(0, 2000)}
            </p>
          )}
        </div>
      )}
    </Card>
  );
}

export function ItemDetailPage() {
  const { t } = useTranslation();
  const { id } = useParams({ from: "/items/$id" });
  const queryClient = useQueryClient();
  const [text, setText] = useState("");

  const { data } = useQuery({
    queryKey: ["item", id],
    queryFn: () => api.workItem(id),
  });

  const send = useMutation({
    mutationFn: (t: string) => api.threadMessage(id, t),
    onSuccess: () => {
      setText("");
      void queryClient.invalidateQueries({ queryKey: ["item", id] });
    },
  });

  if (!data) return <p className="p-6 text-sm text-muted">{t("common.loading")}</p>;
  const { item, events } = data;
  const conversation = conversationEvents(events);
  const executions = executionGroups(events);

  const submit = () => {
    const t = text.trim();
    if (t && !send.isPending) send.mutate(t);
  };

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-border p-4">
        <div className="flex items-center gap-2">
          <h1 className="text-lg font-semibold">{item.title}</h1>
          <Badge tone={item.status === "open" ? "success" : "muted"}>{item.status}</Badge>
        </div>
        <p className="mt-1 text-xs text-muted">
          {t("item.meta", {
            id: item.id,
            time: fmtDateTime(item.createdAt),
            workspace: item.workspace,
          })}
        </p>
      </div>
      <div className="flex-1 space-y-4 overflow-y-auto p-4">
        <section className="space-y-2">
          {conversation.map((e) => (
            <div key={e.id} className={cn("flex", e.kind === "user.message" ? "justify-end" : "justify-start")}>
              <div
                className={cn(
                  "max-w-[85%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap break-words",
                  e.kind === "user.message" ? "bg-primary text-primary-foreground" : "bg-surface-2",
                )}
              >
                {payloadText(e)}
                <div className="mt-1 text-[10px] opacity-60">{fmtTime(e.ts)}</div>
              </div>
            </div>
          ))}
        </section>
        {executions.length > 0 && (
          <section className="space-y-2">
            <h2 className="text-sm font-medium text-muted">{t("item.timeline")}</h2>
            {executions.map((g) => (
              <Execution key={g.executionId} group={g} />
            ))}
          </section>
        )}
      </div>
      <div className="flex gap-2 border-t border-border p-3">
        <Textarea
          rows={2}
          value={text}
          placeholder={t("item.replyPlaceholder")}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
        />
        <Button onClick={submit} disabled={send.isPending} aria-label={t("common.send")}>
          <SendHorizonal className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
