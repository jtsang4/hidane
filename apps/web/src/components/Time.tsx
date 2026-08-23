import { fmtDateTime, fmtRelative } from "../lib/utils.js";
import { useNow } from "../lib/useNow.js";

/**
 * Relative time with the exact value one hover away — the relative form is
 * what you read, the absolute one is what you need when correlating with a log.
 */
export function Time({ iso, className }: { iso: string; className?: string }) {
  const now = useNow();
  return (
    <time dateTime={iso} title={fmtDateTime(iso)} className={className}>
      {fmtRelative(iso, now)}
    </time>
  );
}
