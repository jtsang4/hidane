import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ImagePlus, SendHorizonal, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type HidaneEvent } from "../lib/api.js";
import { acceptableSlice, readImage, type AttachedImage } from "../lib/images.js";
import { conversationEvents, payloadText } from "../lib/grouping.js";
import { pendingState } from "../lib/pending.js";
import { pushToast } from "../lib/toast.js";
import { cn } from "../lib/utils.js";
import { Time } from "../components/Time.js";
import { Badge, Button, Textarea } from "../components/ui/primitives.js";
import { Pending } from "../components/Pending.js";
import { Rich } from "../components/Markdown.js";

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
          "max-w-[85%] rounded-lg px-3 py-2 text-sm break-words",
          mine ? "bg-primary text-primary-foreground" : "bg-surface-2",
          ghost && "opacity-60",
        )}
      >
        {/* Only the agent writes markdown; echoing the user's own text through
            a renderer would silently reformat what they typed. */}
        {mine ? (
          <span className="whitespace-pre-wrap">{payloadText(event)}</span>
        ) : (
          <Rich>{payloadText(event)}</Rich>
        )}
        <div className="mt-1 text-[10px] opacity-60">
          {ghost ? "…" : <Time iso={event.ts} />}
        </div>
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
  const [attached, setAttached] = useState<AttachedImage[]>([]);
  const endRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const attach = async (files: File[]) => {
    const accepted = acceptableSlice(files, attached.length);
    if (accepted.length < files.length) pushToast(t("chat.imageRejected"));
    const read = await Promise.all(accepted.map(readImage));
    setAttached((current) => [...current, ...read]);
  };

  const dropAttachment = (index: number) =>
    setAttached((current) => {
      const target = current[index];
      if (target) URL.revokeObjectURL(target.previewUrl);
      return current.filter((_, i) => i !== index);
    });

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
    mutationFn: ({ body, images }: { body: string; images: AttachedImage[] }) =>
      api.chat(
        body,
        images.map(({ data, mimeType }) => ({ data, mimeType })),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["events", "main"] });
    },
    onError: (err, variables) => {
      // Give the text and the attachments back rather than losing them.
      setOptimistic(null);
      setText((current) => (current.length > 0 ? current : variables.body));
      setAttached((current) => (current.length > 0 ? current : variables.images));
      pushToast(err instanceof ApiError ? err.message : String(err));
    },
  });

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [events.length, optimistic, pending.active]);

  const submit = () => {
    const body = text.trim();
    if ((!body && attached.length === 0) || send.isPending) return;
    setOptimistic(body || t("chat.imageOnly"));
    setText("");
    const images = attached;
    setAttached([]);
    send.mutate({ body, images });
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
      <div className="border-t border-border p-3">
        {attached.length > 0 && (
          <div className="flex flex-wrap gap-2 pb-2">
            {attached.map((image, index) => (
              <div key={image.previewUrl} className="relative">
                <img
                  src={image.previewUrl}
                  alt={image.name}
                  className="h-16 w-16 rounded-md border border-border object-cover"
                />
                <button
                  className="absolute -top-1.5 -right-1.5 rounded-full bg-surface-2 p-0.5 text-muted hover:text-foreground"
                  aria-label={`${t("chat.removeImage")} ${image.name}`}
                  onClick={() => dropAttachment(index)}
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="flex gap-2">
          <input
            ref={fileRef}
            type="file"
            accept="image/*"
            multiple
            className="hidden"
            onChange={(e) => {
              void attach([...(e.target.files ?? [])]);
              e.target.value = "";
            }}
          />
          <Button
            variant="outline"
            size="icon"
            aria-label={t("chat.addImage")}
            onClick={() => fileRef.current?.click()}
          >
            <ImagePlus className="h-4 w-4" />
          </Button>
          <Textarea
            rows={2}
            value={text}
            placeholder={t("chat.placeholder")}
            onChange={(e) => setText(e.target.value)}
            // Pasting a screenshot is how people actually attach images.
            onPaste={(e) => {
              const files = [...e.clipboardData.files];
              if (files.length > 0) {
                e.preventDefault();
                void attach(files);
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <Button
            onClick={submit}
            disabled={send.isPending || (text.trim().length === 0 && attached.length === 0)}
            aria-label={t("common.send")}
          >
            <SendHorizonal className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
