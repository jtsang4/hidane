<script lang="ts">
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { ChevronDown, ChevronRight, Play, Trash2 } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, ApiError, type Schedule } from "../lib/api.js";
  import { pushToast } from "../lib/toast.js";
  import { cn, fmtDateTime } from "../lib/utils.js";
  import RunHistory from "./RunHistory.svelte";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";
  import Card from "./ui/Card.svelte";

  let { schedule }: { schedule: Schedule } = $props();
  const queryClient = useQueryClient();
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["schedules"] });
  const errorText = (error: unknown) => error instanceof ApiError ? error.message : String(error);

  const toggle = createMutation(() => ({
    mutationFn: () => api.updateSchedule(schedule.id, { enabled: !schedule.enabled }),
    onSuccess: invalidate,
    onError: (error) => pushToast(errorText(error)),
  }));
  const run = createMutation(() => ({
    mutationFn: () => api.runSchedule(schedule.id),
    onSuccess: (result) => { pushToast($t("schedules.ranNow", { status: result.status }), "default"); invalidate(); },
    onError: (error) => pushToast(errorText(error)),
  }));
  const remove = createMutation(() => ({
    mutationFn: () => api.deleteSchedule(schedule.id),
    onSuccess: invalidate,
    onError: (error) => pushToast(errorText(error)),
  }));

  let timing = $derived(schedule.cron ? `cron ${schedule.cron}${schedule.timezone ? ` (${schedule.timezone})` : ""}` : $t("schedules.everySec", { s: schedule.intervalSec ?? 0 }));
  let detail = $derived(schedule.action === "http" ? `${schedule.spec.method ?? "GET"} ${schedule.spec.url ?? ""}${schedule.spec.wake ? ` · ${$t("schedules.wakes")}` : ""}` : schedule.spec.prompt ?? "");
</script>

<Card class={cn("space-y-2", !schedule.enabled && "opacity-60")}>
  <div class="flex flex-wrap items-center gap-2">
    <span class="font-medium">{schedule.name}</span>
    <Badge tone={schedule.action === "http" ? "default" : "success"}>{$t(`schedules.action.${schedule.action}` as const)}</Badge>
    <Badge tone="muted">{timing}</Badge>
    <div class="ml-auto flex items-center gap-1">
      <Button variant="ghost" size="icon" aria-label={`${$t("schedules.runNow")} ${schedule.name}`} disabled={run.isPending} onclick={() => run.mutate()}><Play size={16} /></Button>
      <Button variant="ghost" size="icon" aria-label={`${$t("schedules.delete")} ${schedule.name}`} disabled={remove.isPending} onclick={() => { if (confirm($t("schedules.confirmDelete", { name: schedule.name }))) remove.mutate(); }}><Trash2 size={16} class="text-danger" /></Button>
      <Button variant="outline" size="sm" disabled={toggle.isPending} onclick={() => toggle.mutate()}>{schedule.enabled ? $t("schedules.disable") : $t("schedules.enable")}</Button>
    </div>
  </div>
  <p class="text-xs break-all whitespace-pre-wrap text-muted">{detail}</p>
  <p class="text-xs text-muted">{schedule.enabled && schedule.nextRunAt ? $t("schedules.nextRun", { time: fmtDateTime(schedule.nextRunAt) }) : $t("schedules.paused")}{#if schedule.lastRunAt} · {$t("schedules.lastRun", { time: fmtDateTime(schedule.lastRunAt), status: schedule.lastStatus ?? "" })}{/if}</p>
  <RunHistory scheduleId={schedule.id} />
</Card>
