import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Download, FileText } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, authHeaders, type ArtifactEntry } from "../lib/api.js";
import { Time } from "./Time.js";
import { Button, Card } from "./ui/primitives.js";
import { Rich } from "./Markdown.js";

function humanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** Download through fetch so the bearer token can be attached. */
async function download(id: string, path: string): Promise<void> {
  const res = await fetch(
    `/api/work-items/${id}/file?download&path=${encodeURIComponent(path)}`,
    { headers: authHeaders() },
  );
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = path.split("/").pop() ?? "file";
  a.click();
  URL.revokeObjectURL(url);
}

function ArtifactRow({ workItemId, file }: { workItemId: string; file: ArtifactEntry }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const { data, isLoading } = useQuery({
    queryKey: ["artifact", workItemId, file.path],
    queryFn: () => api.workItemFile(workItemId, file.path),
    enabled: open,
  });

  return (
    <div className="border-b border-border py-2 last:border-b-0">
      <div className="flex items-center gap-2">
        <button
          className="flex min-w-0 flex-1 items-center gap-2 text-left text-sm"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
        >
          {open ? (
            <ChevronDown className="h-4 w-4 shrink-0" />
          ) : (
            <ChevronRight className="h-4 w-4 shrink-0" />
          )}
          <FileText className="h-4 w-4 shrink-0 text-muted" />
          <span className="truncate font-mono text-xs">{file.path}</span>
        </button>
        <span className="shrink-0 text-xs text-muted">{humanSize(file.size)}</span>
        <Button
          variant="ghost"
          size="icon"
          aria-label={`${t("item.download")} ${file.path}`}
          onClick={() => void download(workItemId, file.path)}
        >
          <Download className="h-4 w-4" />
        </Button>
      </div>
      {open && (
        <div className="mt-2 space-y-1">
          <p className="text-xs text-muted">
            <Time iso={file.modifiedAt} />
          </p>
          {isLoading && <p className="text-xs text-muted">{t("common.loading")}</p>}
          {data?.reason === "binary" && (
            <p className="text-xs text-muted">{t("item.binaryFile")}</p>
          )}
          {data?.reason === "too-large" && (
            <p className="text-xs text-muted">{t("item.tooLarge")}</p>
          )}
          {data?.text !== undefined &&
            (file.path.endsWith(".md") ? (
              // A produced report is meant to be read, not inspected as source.
              <div className="max-h-96 overflow-auto rounded bg-background p-2 text-xs">
                <Rich>{data.text}</Rich>
              </div>
            ) : (
              <pre className="max-h-96 overflow-auto rounded bg-background p-2 text-xs whitespace-pre-wrap">
                {data.text}
              </pre>
            ))}
        </div>
      )}
    </div>
  );
}

/**
 * Worker output was only ever on the server: reading a produced file meant
 * asking the agent to paste it into chat, which is how an 8000-character reply
 * came to be sent. Artifacts are read here instead.
 */
export function Artifacts({ workItemId }: { workItemId: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const { data } = useQuery({
    queryKey: ["files", workItemId],
    queryFn: () => api.workItemFiles(workItemId),
  });
  const files = data?.files ?? [];

  return (
    <Card className="p-3">
      <button
        className="flex w-full items-center gap-2 text-left text-sm"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        <span className="font-medium">{t("item.files")}</span>
        <span className="text-xs text-muted">({files.length})</span>
      </button>
      {open && (
        <div className="mt-2 border-t border-border pt-2">
          <p className="pb-2 text-xs text-muted">{t("item.filesHint")}</p>
          {files.length === 0 && <p className="text-xs text-muted">{t("item.filesEmpty")}</p>}
          {files.map((f) => (
            <ArtifactRow key={f.path} workItemId={workItemId} file={f} />
          ))}
        </div>
      )}
    </Card>
  );
}
