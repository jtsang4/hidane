import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { elapsedSeconds, type PendingState } from "../lib/pending.js";

/**
 * The runtime answers asynchronously; without a visible wait state the page is
 * indistinguishable from a broken one, and users retype. The elapsed counter
 * matters because an execution legitimately runs for minutes.
 */
export function Pending({ state }: { state: PendingState }) {
  const { t } = useTranslation();
  const [seconds, setSeconds] = useState(() =>
    state.since ? elapsedSeconds(state.since) : 0,
  );

  useEffect(() => {
    if (!state.active || !state.since) return;
    const since = state.since;
    setSeconds(elapsedSeconds(since));
    const timer = setInterval(() => setSeconds(elapsedSeconds(since)), 1000);
    return () => clearInterval(timer);
  }, [state.active, state.since]);

  if (!state.active) return null;
  return (
    <div className="flex justify-start" role="status" aria-live="polite">
      <div className="flex items-center gap-2 rounded-lg bg-surface-2 px-3 py-2 text-sm text-muted">
        <span className="flex gap-1" aria-hidden="true">
          <Dot delay="0ms" />
          <Dot delay="150ms" />
          <Dot delay="300ms" />
        </span>
        {state.phase === "executing" ? t("pending.executing") : t("pending.routing")}
        <span className="text-xs opacity-70">{t("pending.elapsed", { s: seconds })}</span>
      </div>
    </div>
  );
}

function Dot({ delay }: { delay: string }) {
  return (
    <span
      className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted"
      style={{ animationDelay: delay }}
    />
  );
}
