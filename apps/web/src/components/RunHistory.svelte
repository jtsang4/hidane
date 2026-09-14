<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { ChevronDown, ChevronRight } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { cn } from "../lib/utils.js";
  import Time from "./Time.svelte";

  let { scheduleId }: { scheduleId: string } = $props();
  let open = $state(false);
  const runsQuery = createQuery(() => ({
    queryKey: ["schedule-runs", scheduleId],
    queryFn: () => api.scheduleRuns(scheduleId),
    enabled: open,
  }));
  let runs = $derived(runsQuery.data?.runs ?? []);
</script>

<div class="border-t border-border pt-2">
  <button class="flex items-center gap-1 text-xs text-muted" aria-expanded={open} onclick={() => (open = !open)}>
    {#if open}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}{$t("schedules.history")}
  </button>
  {#if open}
    <div class="mt-2 space-y-1">
      {#if runsQuery.isLoading}<p class="text-xs text-muted">{$t("common.loading")}</p>{/if}
      {#if !runsQuery.isLoading && runs.length === 0}<p class="text-xs text-muted">{$t("schedules.historyEmpty")}</p>{/if}
      {#each runs as run (run.id)}
        {@const failed = run.kind === "agent.error" || run.payload["ok"] === false}
        {@const detail = run.kind === "schedule.fired" ? $t("schedules.fired") : String(run.payload["error"] ?? (run.payload["status"] !== undefined ? $t("schedules.httpStatus", { status: String(run.payload["status"]) }) : run.kind))}
        <div class="flex items-center gap-2 text-xs">
          <span class={cn("h-1.5 w-1.5 shrink-0 rounded-full", failed ? "bg-danger" : "bg-success")} aria-hidden="true"></span>
          <Time iso={run.ts} class="shrink-0 text-muted" />
          <span class="truncate text-muted">{detail}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>
