<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { AlarmClock, Plus } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type ScheduleInput } from "../lib/api.js";
  import { toastError } from "../lib/toast.js";
  import EmptyState from "../components/EmptyState.svelte";
  import { segment, segmented, segmentOff, segmentOn } from "../lib/styles.js";
  import { cn } from "../lib/utils.js";
  import Page from "../components/Page.svelte";
  import ScheduleCard from "../components/ScheduleCard.svelte";
  import Button from "../components/ui/Button.svelte";
  import Card from "../components/ui/Card.svelte";
  import Checkbox from "../components/ui/Checkbox.svelte";
  import Input from "../components/ui/Input.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  const queryClient = useQueryClient();
  let creating = $state(false);
  let form = $state({
    name: "",
    action: "prompt" as "prompt" | "http",
    timing: "interval" as "interval" | "cron",
    intervalSec: "3600",
    cron: "",
    timezone: "",
    prompt: "",
    url: "",
    wake: false,
  });
  const schedulesQuery = createQuery(() => ({ queryKey: ["schedules"], queryFn: () => api.schedules() }));
  let schedules = $derived(schedulesQuery.data?.schedules ?? []);
  const create = createMutation(() => ({
    mutationFn: (input: ScheduleInput) => api.createSchedule(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["schedules"] });
      form = { name: "", action: "prompt", timing: "interval", intervalSec: "3600", cron: "", timezone: "", prompt: "", url: "", wake: false };
      creating = false;
    },
    onError: (error) => toastError(error),
  }));

  function submit(): void {
    const input: ScheduleInput = {
      name: form.name,
      action: form.action,
      spec: form.action === "prompt" ? { prompt: form.prompt } : { url: form.url, wake: form.wake },
      ...(form.timing === "cron" ? { cron: form.cron, ...(form.timezone ? { timezone: form.timezone } : {}) } : { intervalSec: Number(form.intervalSec) }),
    };
    create.mutate(input);
  }
</script>

<Page title={$t("schedules.title")}>
  {#snippet actions()}
    {#if !creating}<Button variant="soft" onclick={() => (creating = true)}><Plus size={16} />{$t("schedules.new")}</Button>{/if}
  {/snippet}
  <p class="text-xs text-muted">{$t("schedules.subtitle")}</p>
  {#if creating}
    <Card class="space-y-3" aria-label={$t("schedules.new")}>
      <Input bind:value={form.name} placeholder={$t("schedules.form.name")} />
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted">{$t("schedules.form.actionLabel")}</span>
          <div class={segmented}>
            <button type="button" class={cn(segment, form.action === "prompt" ? segmentOn : segmentOff)} aria-pressed={form.action === "prompt"} onclick={() => (form.action = "prompt")}>{$t("schedules.action.prompt")}</button>
            <button type="button" class={cn(segment, form.action === "http" ? segmentOn : segmentOff)} aria-pressed={form.action === "http"} onclick={() => (form.action = "http")}>{$t("schedules.action.http")}</button>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted">{$t("schedules.form.timingLabel")}</span>
          <div class={segmented}>
            <button type="button" class={cn(segment, form.timing === "interval" ? segmentOn : segmentOff)} aria-pressed={form.timing === "interval"} onclick={() => (form.timing = "interval")}>{$t("schedules.form.interval")}</button>
            <button type="button" class={cn(segment, form.timing === "cron" ? segmentOn : segmentOff)} aria-pressed={form.timing === "cron"} onclick={() => (form.timing = "cron")}>{$t("schedules.cron")}</button>
          </div>
        </div>
      </div>
      {#if form.timing === "interval"}
        <Input type="number" min={10} bind:value={form.intervalSec} placeholder={$t("schedules.form.intervalSec")} />
      {:else}
        <div class="flex flex-col gap-2 sm:flex-row"><Input bind:value={form.cron} placeholder={$t("schedules.form.cron")} /><Input class="sm:w-44" bind:value={form.timezone} placeholder={$t("schedules.form.timezone")} /></div>
      {/if}
      {#if form.action === "prompt"}
        <Textarea rows={3} bind:value={form.prompt} placeholder={$t("schedules.form.prompt")} />
      {:else}
        <div class="space-y-2"><Input bind:value={form.url} placeholder={$t("schedules.form.url")} /><label class="flex items-center gap-2 text-sm text-muted"><Checkbox bind:checked={form.wake} />{$t("schedules.form.wake")}</label></div>
      {/if}
      <div class="flex justify-end gap-2"><Button variant="secondary" onclick={() => (creating = false)}>{$t("common.cancel")}</Button><Button disabled={create.isPending} onclick={submit}>{$t("schedules.form.create")}</Button></div>
    </Card>
  {/if}
  {#if schedulesQuery.isLoading}
    <p class="flex items-center gap-2 text-sm text-muted"><span class="size-3 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {/if}
  {#if !schedulesQuery.isLoading && schedules.length === 0 && !creating}
    <EmptyState icon={AlarmClock} text={$t("schedules.empty")}>
      <Button variant="secondary" onclick={() => (creating = true)}><Plus />{$t("schedules.new")}</Button>
    </EmptyState>
  {/if}
  {#each schedules as schedule (schedule.id)}<ScheduleCard {schedule} />{/each}
</Page>
