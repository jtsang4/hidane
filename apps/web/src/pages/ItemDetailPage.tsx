import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, SendHorizonal } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type HidaneEvent } from "../lib/api.js";
import {
  conversationEvents,
  executionGroups,
  payloadText,
  type ExecutionGroup,
} from "../lib/grouping.js";
import { pendingState } from "../lib/pending.js";
import { pushToast } from "../lib/toast.js";
import { cn, fmtTime, fmtDateTime } from "../lib/utils.js";
import { Badge, Button, Card, Textarea } from "../components/ui/primitives.js";
import { Pending } from "../components/Pending.js";

function Execution({ group }: { group: ExecutionGroup }) {
  const { t } = useTranslation();
  // A running execution is the thing the user most wants to look inside.
  const [open, setOpen] = useState(group.ok === null);
  const tone = group.ok === null ? "muted" : group.ok ? "success" : "danger";
  const label =
    group.ok === null ? t("item.running") : group.ok ? t("item.success") : t("item.failed");
  return (
    <Card className="p-3">
      <button
        className="flex w-full items-center gap-2 text-left text-sm"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
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
  const [optimistic, setOptimistic] = useState<string | null>(null);

  const { data, error } = useQuery({
    queryKey: ["item", id],
    queryFn: () => api.workItem(id),
    retry: false,
  });

  const events = data?.events ?? [];
  const conversation = conversationEvents(events);
  const pending = pendingState(events);

  useEffect(() => {
    if (!optimistic) return;
    if (conversation.some((e) => e.kind === "user.message" && payloadText(e) === optimistic)) {
      setOptimistic(null);
    }
  }, [conversation, optimistic]);

  const send = useMutation({
    mutationFn: (body: string) => api.threadMessage(id, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["item", id] });
    },
    onError: (err, body) => {
      setOptimistic(null);
      setText((current) => (current.length > 0 ? current : body));
      pushToast(err instanceof ApiError ? err.message : String(err));
    },
  });

  const setStatus = useMutation({
    mutationFn: (status: "open" | "done") => api.setWorkItemStatus(id, status),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["item", id] });
      void queryClient.invalidateQueries({ queryKey: ["items"] });
    },
    onError: (err) => pushToast(err instanceof ApiError ? err.message : String(err)),
  });

  if (error) return <p className="p-6 text-sm text-muted">{t("item.notFound")}</p>;
  if (!data) return <p className="p-6 text-sm text-muted">{t("common.loading")}</p>;
  const { item } = data;
  const executions = executionGroups(events);

  const submit = () => {
    const body = text.trim();
    if (!body || send.isPending) return;
    setOptimistic(body);
    setText("");
    send.mutate(body);
  };

  const waiting = pending.active
    ? pending
    : optimistic !== null
      ? { active: true, since: new Date().toISOString(), phase: "routing" as const }
      : pending;

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-border p-4">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">{item.title}</h1>
          <Badge tone={item.status === "open" ? "success" : "muted"}>{item.status}</Badge>
          <Button
            className="ml-auto"
            variant="outline"
            size="sm"
            disabled={setStatus.isPending}
            onClick={() => setStatus.mutate(item.status === "open" ? "done" : "open")}
          >
            {item.status === "open" ? t("item.markDone") : t("item.reopen")}
          </Button>
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
            <div
              key={e.id}
              className={cn("flex", e.kind === "user.message" ? "justify-end" : "justify-start")}
            >
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
          {optimistic && (
            <div className="flex justify-end">
              <div className="max-w-[85%] rounded-lg bg-primary px-3 py-2 text-sm whitespace-pre-wrap break-words text-primary-foreground opacity-60">
                {optimistic}
                <div className="mt-1 text-[10px] opacity-60">…</div>
              </div>
            </div>
          )}
          <Pending state={waiting} />
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
      <div className="border-t border-border p-3">
        {pending.phase === "executing" && (
          <p className="pb-2 text-xs text-muted">{t("item.steerHint")}</p>
        )}
        <div className="flex gap-2">
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
          <Button
            onClick={submit}
            disabled={send.isPending || text.trim().length === 0}
            aria-label={t("common.send")}
          >
            <SendHorizonal className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
