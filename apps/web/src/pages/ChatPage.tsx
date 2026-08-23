import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { SendHorizonal } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type HidaneEvent } from "../lib/api.js";
import { conversationEvents, payloadText } from "../lib/grouping.js";
import { pendingState } from "../lib/pending.js";
import { pushToast } from "../lib/toast.js";
import { cn, fmtTime } from "../lib/utils.js";
import { Badge, Button, Textarea } from "../components/ui/primitives.js";
import { Pending } from "../components/Pending.js";

function Bubble({ event, ghost = false }: { event: HidaneEvent; ghost?: boolean }) {
  if (event.kind === "escalation") {
    return (
      <div className="flex justify-center">
        <Badge tone="default">
          ↑ {payloadText(event)}
          {event.workItemId && (
            <Link to="/items/$id" params={{ id: event.workItemId }} className="ml-1 underline">
              {event.workItemId}
            </Link>
          )}
        </Badge>
      </div>
    );
  }
  if (event.kind === "agent.error") {
    return (
      <div className="flex justify-center">
        <Badge tone="danger">{payloadText(event)}</Badge>
      </div>
    );
  }
  const mine = event.kind === "user.message";
  return (
    <div className={cn("flex", mine ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "max-w-[85%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap break-words",
          mine ? "bg-primary text-primary-foreground" : "bg-surface-2",
          ghost && "opacity-60",
        )}
      >
        {payloadText(event)}
        <div className="mt-1 text-[10px] opacity-60">{ghost ? "…" : fmtTime(event.ts)}</div>
      </div>
    </div>
  );
}

export function ChatPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  /** Held until the same text comes back from the log — POST only returns 202. */
  const [optimistic, setOptimistic] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);

  const { data } = useQuery({
    queryKey: ["events", "main"],
    queryFn: () => api.events({ thread: "main", tail: 200 }),
  });
  const all = data?.events ?? [];
  const events = conversationEvents(all);
  const pending = pendingState(all);

  // Drop the ghost once the real event lands, whatever recorded it.
  useEffect(() => {
    if (!optimistic) return;
    if (events.some((e) => e.kind === "user.message" && payloadText(e) === optimistic)) {
      setOptimistic(null);
    }
  }, [events, optimistic]);

  const send = useMutation({
    mutationFn: (body: string) => api.chat(body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["events", "main"] });
    },
    onError: (err, body) => {
      // Give the text back rather than losing what the user typed.
      setOptimistic(null);
      setText((current) => (current.length > 0 ? current : body));
      pushToast(err instanceof ApiError ? err.message : String(err));
    },
  });

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [events.length, optimistic, pending.active]);

  const submit = () => {
    const body = text.trim();
    if (!body || send.isPending) return;
    setOptimistic(body);
    setText("");
    send.mutate(body);
  };

  // While the ghost is up the log has not caught up yet, so show the wait state
  // from the local send too — otherwise the first second reads as "nothing happened".
  const waiting = pending.active
    ? pending
    : optimistic !== null
      ? { active: true, since: new Date().toISOString(), phase: "routing" as const }
      : pending;

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 space-y-3 overflow-y-auto p-4">
        {events.length === 0 && !optimistic && (
          <p className="pt-16 text-center text-sm text-muted">{t("chat.empty")}</p>
        )}
        {events.map((e) => (
          <Bubble key={e.id} event={e} />
        ))}
        {optimistic && (
          <Bubble
            ghost
            event={
              {
                id: "optimistic",
                kind: "user.message",
                ts: new Date().toISOString(),
                payload: { text: optimistic },
              } as unknown as HidaneEvent
            }
          />
        )}
        <Pending state={waiting} />
        <div ref={endRef} />
      </div>
      <div className="flex gap-2 border-t border-border p-3">
        <Textarea
          rows={2}
          value={text}
          placeholder={t("chat.placeholder")}
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
  );
}
