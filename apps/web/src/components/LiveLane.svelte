<script lang="ts">
  import { useQueryClient } from "@tanstack/svelte-query";
  import { eventStreamUrl } from "../lib/api.js";
  import { livenessFrom, shouldReconnect, type LiveState } from "../lib/live.js";

  let {
    enabled,
    onstatechange,
  }: { enabled: boolean; onstatechange?: (state: LiveState) => void } = $props();

  const queryClient = useQueryClient();
  let liveState = $state<LiveState>("connecting");
  let attempt = $state(0);

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
      window.dispatchEvent(new MessageEvent("hidane:event", { data: (event as MessageEvent<string>).data }));
      void queryClient.invalidateQueries();
    });

    const timer = window.setInterval(() => {
      const now = Date.now();
      liveState = livenessFrom(lastHeardAt, everHeard, now);
      if (shouldReconnect(lastHeardAt, connectedAt, now)) attempt += 1;
    }, 5_000);

    return () => {
      window.clearInterval(timer);
      source.close();
    };
  });

  $effect(() => {
    onstatechange?.(liveState);
  });
</script>
