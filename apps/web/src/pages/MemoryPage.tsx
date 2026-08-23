import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type MemoryEntry } from "../lib/api.js";
import { pushToast } from "../lib/toast.js";
import { Badge, Button, Card } from "../components/ui/primitives.js";

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

  const entries: MemoryEntry[] = data?.entries ?? [];

  return (
    <div className="space-y-3 p-4">
      <div>
        <h1 className="text-lg font-semibold">{t("memory.title")}</h1>
        <p className="mt-1 text-xs break-all text-muted">
          {t("memory.subtitle", { path: data?.path ?? "" })}
        </p>
      </div>
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
