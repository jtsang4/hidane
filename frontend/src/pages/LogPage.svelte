<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { ChevronLeft, ChevronRight } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { shiftDay, today } from "../lib/utils.js";
  import Markdown from "../components/Markdown.svelte";
  import Page from "../components/Page.svelte";
  import Button from "../components/ui/Button.svelte";
  import DatePicker from "../components/ui/DatePicker.svelte";

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

<Page title={$t("log.title")}>
  {#snippet actions()}
    <Button variant="ghost" size="icon-sm" aria-label={$t("log.prev")} onclick={() => (day = shiftDay(day, -1))}><ChevronLeft size={16} /></Button>
    <DatePicker label={$t("log.day")} value={day} max={today()} onchange={(next) => (day = next)} />
    <Button variant="ghost" size="icon-sm" aria-label={$t("log.next")} disabled={isToday} onclick={() => (day = shiftDay(day, 1))}><ChevronRight size={16} /></Button>
    <Button variant="outline" size="sm" disabled={isToday} onclick={() => (day = today())}>{$t("log.today")}</Button>
  {/snippet}
  {#if logQuery.isLoading}
    <p class="flex items-center gap-2 px-1 text-sm text-muted"><span class="h-3 w-3 animate-spin rounded-full border-2 border-muted border-t-transparent"></span>{$t("common.loading")}</p>
  {/if}
  {#if empty}<p class="py-16 text-center text-sm text-muted">{$t("log.empty")}</p>{/if}
  <Markdown content={logQuery.data?.markdown ?? ""} class="text-sm [&_code]:break-all" />
</Page>
