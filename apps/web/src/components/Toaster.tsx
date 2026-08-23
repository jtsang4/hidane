import { useSyncExternalStore } from "react";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "../lib/utils.js";
import { dismissToast, getToasts, subscribeToasts } from "../lib/toast.js";

export function Toaster() {
  const { t } = useTranslation();
  const toasts = useSyncExternalStore(subscribeToasts, getToasts, getToasts);
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-50 flex flex-col items-center gap-2 px-4">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role="alert"
          className={cn(
            "pointer-events-auto flex w-full max-w-md items-start gap-3 rounded-lg border px-3 py-2 text-sm shadow-lg",
            toast.tone === "danger"
              ? "border-danger/40 bg-danger/10 text-danger"
              : "border-border bg-surface",
          )}
        >
          <span className="min-w-0 flex-1 break-words">{toast.message}</span>
          <button
            className="shrink-0 opacity-70 hover:opacity-100"
            aria-label={t("common.dismiss")}
            onClick={() => dismissToast(toast.id)}
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      ))}
    </div>
  );
}
