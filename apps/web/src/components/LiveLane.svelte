<script lang="ts">
  import { useQueryClient } from "@tanstack/svelte-query";
  import { eventStreamUrl } from "../lib/api.js";
  import { applyLiveFrame, noteLiveEvent } from "../lib/liveText.js";
  import { invalidationFor, livenessFrom, shouldReconnect, type LiveState } from "../lib/live.js";

  let {
    enabled,
    onstatechange,
  }: { enabled: boolean; onstatechange?: (state: LiveState) => void } = $props();

  const queryClient = useQueryClient();
  let liveState = $state<LiveState>("connecting");
  let attempt = $state(0);

  /** Lists and the board refetch at most this often while a worker is busy. */
  const THROTTLE_MS = 700;
  const throttled = new Map<string, string[]>();
  let flushTimer: number | undefined;

  function refresh(event: { kind: string; threadId: string | null; workItemId: string | null } | null): void {
    // An unparseable frame still means something changed: refetch everything.
    if (!event) {
      void queryClient.invalidateQueries();
      return;
    }
    const { now, throttled: later } = invalidationFor(event);
    for (const key of now) void queryClient.invalidateQueries({ queryKey: key });
    for (const key of later) throttled.set(JSON.stringify(key), key);
    flushTimer ??= window.setTimeout(() => {
      flushTimer = undefined;
      for (const key of throttled.values()) void queryClient.invalidateQueries({ queryKey: key });
      throttled.clear();
    }, THROTTLE_MS);
  }

  $effect(() => {
    const currentAttempt = attempt;
    void currentAttempt;
    if (!enabled) {
      liveState = "connecting";
      return;
    }

    const connectedAt = Date.now();
    let lastHeardAt = Date.now();
    let everHeard = false;
    const source = new EventSource(eventStreamUrl());
    const heard = () => {
      lastHeardAt = Date.now();
      everHeard = true;
      liveState = "live";
    };
    source.addEventListener("hello", heard);
    source.addEventListener("ping", heard);
    source.addEventListener("hidane", (event) => {
      heard();
      const data = (event as MessageEvent<string>).data;
      window.dispatchEvent(new MessageEvent("hidane:event", { data }));
      let parsed: { seq: number; kind: string; threadId: string | null; workItemId: string | null } | null = null;
      try {
        parsed = JSON.parse(data) as typeof parsed;
        if (parsed) noteLiveEvent(parsed);
      } catch {
        // A frame we cannot parse still proves the stream is alive.
      }
      refresh(parsed);
    });
    // Text of a reply still being written. Deliberately no invalidation: these
    // arrive per token, and refetching a page of history for each one would be
    // pathological. They are ephemeral and never enter the event log.
    source.addEventListener("stream", (event) => {
      heard();
      applyLiveFrame((event as MessageEvent<string>).data);
    });

    const timer = window.setInterval(() => {
      const now = Date.now();
      liveState = livenessFrom(lastHeardAt, everHeard, now);
      if (shouldReconnect(lastHeardAt, connectedAt, now)) attempt += 1;
    }, 5_000);

    return () => {
      window.clearInterval(timer);
      window.clearTimeout(flushTimer);
      flushTimer = undefined;
      source.close();
    };
  });

  $effect(() => {
    onstatechange?.(liveState);
  });
</script>
