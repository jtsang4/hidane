<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { ChevronLeft, ChevronRight } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { shiftDay, today } from "../lib/utils.js";
  import Markdown from "../components/Markdown.svelte";
  import Button from "../components/ui/Button.svelte";
  import Input from "../components/ui/Input.svelte";

  let day = $state(today());
  let valid = $derived(/^\d{4}-\d{2}-\d{2}$/.test(day));
  const logQuery = createQuery(() => ({
    queryKey: ["worklog", day],
    queryFn: () => api.worklog(day),
    enabled: valid,
  }));
  let isToday = $derived(day === today());
  let empty = $derived(logQuery.data !== undefined && logQuery.data.eventCount === 0);
</script>

<div class="space-y-3 p-4">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-lg font-semibold">{$t("log.title")}</h1>
    <div class="flex flex-wrap items-center gap-2">
      <Button variant="outline" size="icon" aria-label={$t("log.prev")} onclick={() => (day = shiftDay(day, -1))}><ChevronLeft size={16} /></Button>
      <Input type="date" class="w-36 sm:w-40" bind:value={day} />
      <Button variant="outline" size="icon" aria-label={$t("log.next")} disabled={isToday} onclick={() => (day = shiftDay(day, 1))}><ChevronRight size={16} /></Button>
      <Button variant="outline" size="sm" disabled={isToday} onclick={() => (day = today())}>{$t("log.today")}</Button>
    </div>
  </div>
  {#if logQuery.isLoading}<p class="text-sm text-muted">{$t("common.loading")}</p>{/if}
  {#if empty}<p class="pt-8 text-center text-sm text-muted">{$t("log.empty")}</p>{/if}
  <Markdown content={logQuery.data?.markdown ?? ""} class="text-sm [&_code]:break-all" />
</div>
