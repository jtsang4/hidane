<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Plus } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, type ScheduleInput } from "../lib/api.js";
  import { errorText } from "../lib/settings.js";
  import { pushToast } from "../lib/toast.js";
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
    onError: (error) => pushToast(errorText(error)),
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
    {#if !creating}<Button size="sm" onclick={() => (creating = true)}><Plus size={16} />{$t("schedules.new")}</Button>{/if}
  {/snippet}
  <p class="px-1 text-xs text-muted">{$t("schedules.subtitle")}</p>
  {#if creating}
    <Card class="space-y-3" aria-label={$t("schedules.new")}>
      <Input bind:value={form.name} placeholder={$t("schedules.form.name")} />
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-xs text-muted">{$t("schedules.form.actionLabel")}</span>
        <Button variant={form.action === "prompt" ? "default" : "outline"} size="sm" onclick={() => (form.action = "prompt")}>{$t("schedules.action.prompt")}</Button>
        <Button variant={form.action === "http" ? "default" : "outline"} size="sm" onclick={() => (form.action = "http")}>{$t("schedules.action.http")}</Button>
        <span class="ml-3 text-xs text-muted">{$t("schedules.form.timingLabel")}</span>
        <Button variant={form.timing === "interval" ? "default" : "outline"} size="sm" onclick={() => (form.timing = "interval")}>{$t("schedules.form.interval")}</Button>
        <Button variant={form.timing === "cron" ? "default" : "outline"} size="sm" onclick={() => (form.timing = "cron")}>{$t("schedules.cron")}</Button>
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
      <div class="flex justify-end gap-2"><Button variant="outline" size="sm" onclick={() => (creating = false)}>{$t("common.cancel")}</Button><Button size="sm" disabled={create.isPending} onclick={submit}>{$t("schedules.form.create")}</Button></div>
    </Card>
  {/if}
  {#if schedulesQuery.isLoading}
    <p class="flex items-center gap-2 px-1 text-sm text-muted"><span class="h-3 w-3 animate-spin rounded-full border-2 border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {/if}
  {#if !schedulesQuery.isLoading && schedules.length === 0 && !creating}
    <div class="flex flex-col items-center gap-3 py-16">
      <p class="text-sm text-muted">{$t("schedules.empty")}</p>
      <Button size="sm" variant="outline" onclick={() => (creating = true)}><Plus size={14} />{$t("schedules.new")}</Button>
    </div>
  {/if}
  {#each schedules as schedule (schedule.id)}<ScheduleCard {schedule} />{/each}
</Page>
