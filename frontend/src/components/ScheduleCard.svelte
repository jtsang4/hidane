<script lang="ts">
  import { createMutation, useQueryClient } from "@tanstack/svelte-query";
  import { ChevronDown, ChevronRight, Play, Trash2 } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import i18n from "../i18n/index.js";
  import { api, type Schedule } from "../lib/api.js";
  import { confirmAction } from "../lib/confirm.svelte.js";
  import { pushToast, toastError } from "../lib/toast.js";
  import { cn, fmtDateTime } from "../lib/utils.js";
  import RunHistory from "./RunHistory.svelte";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";
  import Card from "./ui/Card.svelte";
  import Switch from "./ui/Switch.svelte";

  let { schedule }: { schedule: Schedule } = $props();
  const queryClient = useQueryClient();
  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["schedules"] });

  const toggle = createMutation(() => ({
    mutationFn: () => api.updateSchedule(schedule.id, { enabled: !schedule.enabled }),
    onSuccess: invalidate,
    onError: (error) => toastError(error),
  }));
  const run = createMutation(() => ({
    mutationFn: () => api.runSchedule(schedule.id),
    onSuccess: (result) => { pushToast(i18n.t("schedules.ranNow", { status: result.status }), "default"); invalidate(); },
    onError: (error) => toastError(error),
  }));
  const remove = createMutation(() => ({
    mutationFn: () => api.deleteSchedule(schedule.id),
    onSuccess: invalidate,
    onError: (error) => toastError(error),
  }));

  async function confirmRemove(): Promise<void> {
    const confirmed = await confirmAction({
      title: i18n.t("schedules.confirmDelete", { name: schedule.name }),
      confirmLabel: i18n.t("common.delete"),
      destructive: true,
    });
    if (confirmed) remove.mutate();
  }

  let interval = $derived($t("schedules.everySec", { s: schedule.intervalSec ?? 0 }));
  let detail = $derived(schedule.action === "http" ? `${schedule.spec.method ?? "GET"} ${schedule.spec.url ?? ""}${schedule.spec.wake ? ` · ${$t("schedules.wakes")}` : ""}` : schedule.spec.prompt ?? "");
</script>

<Card class="space-y-2">
  <!-- Wide: name, kind, timing, then the actions and the switch on one line. A phone keeps
       the name and its switch together on the first line and moves the rest under them. -->
  <div class="flex flex-wrap items-center gap-2">
    <span class={cn("order-1 font-medium", !schedule.enabled && "text-muted")}>{schedule.name}</span>
    <span class="order-2 ml-auto flex items-center sm:order-5 sm:ml-1.5"><Switch checked={schedule.enabled} label={`${$t("schedules.enable")} ${schedule.name}`} disabled={toggle.isPending} onchange={() => toggle.mutate()} /></span>
    <span class="order-3 basis-full sm:hidden" aria-hidden="true"></span>
    <Badge tone="muted" class="order-4 sm:order-2">{$t(`schedules.action.${schedule.action}` as const)}</Badge>
    {#if schedule.cron}
      <!-- The expression is a machine token, set in mono like every id; the zone beside it is plain words. -->
      <Badge tone="muted" class="order-4 sm:order-3"><span class="font-mono">{schedule.cron}</span>{#if schedule.timezone}<span class="text-muted/80">{schedule.timezone}</span>{/if}</Badge>
    {:else}
      <Badge tone="muted" class="order-4 sm:order-3">{interval}</Badge>
    {/if}
    <div class="order-4 ml-auto flex items-center gap-1 sm:order-4">
      <Button variant="ghost" size="icon" aria-label={`${$t("schedules.runNow")} ${schedule.name}`} disabled={run.isPending} onclick={() => run.mutate()}><Play size={16} /></Button>
      <Button variant="ghost" size="icon" class="hover:bg-danger/10 hover:text-danger" aria-label={`${$t("schedules.delete")} ${schedule.name}`} disabled={remove.isPending} onclick={() => void confirmRemove()}><Trash2 /></Button>
    </div>
  </div>
  <p class="text-xs break-all whitespace-pre-wrap text-muted select-text">{detail}</p>
  <p class="text-xs text-muted">{schedule.enabled && schedule.nextRunAt ? $t("schedules.nextRun", { time: fmtDateTime(schedule.nextRunAt) }) : $t("schedules.paused")}{#if schedule.lastRunAt}{` · ${$t("schedules.lastRun", { time: fmtDateTime(schedule.lastRunAt), status: schedule.lastStatus ?? "" })}`}{/if}</p>
  <RunHistory scheduleId={schedule.id} />
</Card>
