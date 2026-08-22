import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { SendHorizonal } from "lucide-react";
import { api, type HidaneEvent } from "../lib/api.js";
import { conversationEvents, payloadText } from "../lib/grouping.js";
import { cn, fmtTime } from "../lib/utils.js";
import { Badge, Button, Textarea } from "../components/ui/primitives.js";

function Bubble({ event }: { event: HidaneEvent }) {
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
        )}
      >
        {payloadText(event)}
        <div className={cn("mt-1 text-[10px] opacity-60")}>{fmtTime(event.ts)}</div>
      </div>
    </div>
  );
}

export function ChatPage() {
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const endRef = useRef<HTMLDivElement>(null);

  const { data } = useQuery({
    queryKey: ["events", "main"],
    queryFn: () => api.events({ thread: "main", tail: 200 }),
  });
  const events = conversationEvents(data?.events ?? []);

  const send = useMutation({
    mutationFn: (t: string) => api.chat(t),
    onSuccess: () => {
      setText("");
      void queryClient.invalidateQueries({ queryKey: ["events", "main"] });
    },
  });

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [data?.events.length]);

  const submit = () => {
    const t = text.trim();
    if (t && !send.isPending) send.mutate(t);
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 space-y-3 overflow-y-auto p-4">
        {events.length === 0 && (
          <p className="pt-16 text-center text-sm text-muted">
            对 Primary 说点什么——它会回复、建工作项或路由到已有工作项。
          </p>
        )}
        {events.map((e) => (
          <Bubble key={e.id} event={e} />
        ))}
        <div ref={endRef} />
      </div>
      <div className="flex gap-2 border-t border-border p-3">
        <Textarea
          rows={2}
          value={text}
          placeholder="消息 Primary…（Enter 发送，Shift+Enter 换行）"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
        />
        <Button onClick={submit} disabled={send.isPending} aria-label="发送">
          <SendHorizonal className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
