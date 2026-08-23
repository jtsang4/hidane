import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type MemoryEntry } from "../lib/api.js";
import { pushToast } from "../lib/toast.js";
import { Badge, Button, Card, Textarea } from "../components/ui/primitives.js";

const KIND_TONE = {
  fact: "muted",
  preference: "default",
  decision: "success",
  lesson: "danger",
} as const;

/**
 * Memory is what makes the runtime persistent, and it is injected into every
 * routing decision — so a stale or wrong entry silently steers future work.
 * Reviewing and forgetting entries needs to be as easy as reading them.
 */
export function MemoryPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["memories"],
    queryFn: () => api.memories(),
  });

  const forget = useMutation({
    mutationFn: (id: string) => api.forgetMemory(id),
    onSuccess: () => {
      pushToast(t("memory.forgotten"), "default");
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (err) => pushToast(err instanceof ApiError ? err.message : String(err)),
  });

  // Distillation is the automatic path, but someone who already knows a
  // preference should not have to hint at it and hope the distiller notices.
  const [draft, setDraft] = useState<string | null>(null);
  const [kind, setKind] = useState<MemoryEntry["kind"]>("preference");
  const add = useMutation({
    mutationFn: (content: string) => api.addMemory(kind, content),
    onSuccess: () => {
      pushToast(t("memory.added"), "default");
      setDraft(null);
      void queryClient.invalidateQueries({ queryKey: ["memories"] });
    },
    onError: (err) => pushToast(err instanceof ApiError ? err.message : String(err)),
  });

  const entries: MemoryEntry[] = data?.entries ?? [];

  return (
    <div className="space-y-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-lg font-semibold">{t("memory.title")}</h1>
          <p className="mt-1 text-xs break-all text-muted">
            {t("memory.subtitle", { path: data?.path ?? "" })}
          </p>
        </div>
        {draft === null && (
          <Button size="sm" onClick={() => setDraft("")}>
            <Plus className="h-4 w-4" /> {t("memory.add")}
          </Button>
        )}
      </div>
      {draft !== null && (
        <Card className="space-y-2">
          <div className="flex flex-wrap gap-2">
            {(["fact", "preference", "decision", "lesson"] as const).map((k) => (
              <Button
                key={k}
                size="sm"
                variant={kind === k ? "default" : "outline"}
                onClick={() => setKind(k)}
              >
                {t(`memory.kind.${k}` as const)}
              </Button>
            ))}
          </div>
          <Textarea
            rows={2}
            autoFocus
            placeholder={t("memory.addPlaceholder")}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
          />
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" onClick={() => setDraft(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              size="sm"
              disabled={add.isPending || draft.trim().length === 0}
              onClick={() => add.mutate(draft.trim())}
            >
              {t("memory.save")}
            </Button>
          </div>
        </Card>
      )}
      {isLoading && <p className="text-sm text-muted">{t("common.loading")}</p>}
      {!isLoading && entries.length === 0 && (
        <p className="pt-8 text-center text-sm text-muted">{t("memory.empty")}</p>
      )}
      {entries.map((entry) => (
        <Card key={entry.id} className="flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={KIND_TONE[entry.kind] ?? "muted"}>
                {t(`memory.kind.${entry.kind}` as const)}
              </Badge>
              <span className="text-xs text-muted">{entry.date}</span>
              <span className="font-mono text-xs text-muted">{entry.id}</span>
            </div>
            <p className="mt-2 text-sm break-words whitespace-pre-wrap">{entry.content}</p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`${t("memory.forget")} ${entry.id}`}
            disabled={forget.isPending}
            onClick={() => {
              if (confirm(t("memory.confirm"))) forget.mutate(entry.id);
            }}
          >
            <Trash2 className="h-4 w-4 text-danger" />
          </Button>
        </Card>
      ))}
    </div>
  );
}
