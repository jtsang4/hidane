<script lang="ts">
  import { elapsedSeconds, type PendingState } from "../lib/pending.js";
  import { t } from "../i18n/index.js";

  let { state: pending }: { state: PendingState } = $props();
  let seconds = $state(0);

  $effect(() => {
    if (!pending.active || !pending.since) return;
    const since = pending.since;
    seconds = elapsedSeconds(since);
    const timer = window.setInterval(() => {
      seconds = elapsedSeconds(since);
    }, 1000);
    return () => window.clearInterval(timer);
  });
</script>

{#if pending.active}
  <div class="flex justify-start" role="status" aria-live="polite">
    <div class="flex items-center gap-2 rounded-lg bg-surface-2 px-3 py-2 text-sm text-muted">
      <span class="flex gap-1" aria-hidden="true">
        <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-muted" style="animation-delay: 0ms"></span>
        <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-muted" style="animation-delay: 150ms"></span>
        <span class="h-1.5 w-1.5 animate-bounce rounded-full bg-muted" style="animation-delay: 300ms"></span>
      </span>
      {$t(pending.phase === "executing" ? "pending.executing" : "pending.routing")}
      <span class="text-xs opacity-70">{$t("pending.elapsed", { s: seconds })}</span>
    </div>
  </div>
{/if}
