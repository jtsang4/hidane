import { useEffect, useState } from "react";

/**
 * A ticking clock for relative timestamps.
 *
 * Without it "刚刚" stays "刚刚" until something else happens to re-render, so
 * an idle page quietly lies about how old its contents are.
 */
export function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    // A tab in the background stops receiving timers reliably; catch up on return.
    const onVisible = () => {
      if (document.visibilityState === "visible") setNow(Date.now());
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [intervalMs]);
  return now;
}
