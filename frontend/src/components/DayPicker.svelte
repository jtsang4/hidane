<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Popover } from "bits-ui";
  import CalendarDays from "@lucide/svelte/icons/calendar-days";
  import { language, t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { daysByMonth } from "../lib/history.js";
  import { popover, popoverItem, popoverLabel, toolbarButton } from "../lib/styles.js";
  import { cn, fmtDay, fmtMonth } from "../lib/utils.js";

  let { onpick }: { onpick: (eventId: string) => void } = $props();

  let open = $state(false);
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;

  const days = createQuery(() => ({
    queryKey: ["conversation-days", zone],
    queryFn: () => api.conversationDays(zone),
    enabled: open,
    staleTime: 30_000,
  }));

  let groups = $derived(daysByMonth(days.data?.days ?? []));

  function pick(eventId: string): void {
    open = false;
    onpick(eventId);
  }
</script>

<Popover.Root bind:open>
  <Popover.Trigger
    class={cn(toolbarButton, open && "bg-accent text-foreground")}
    aria-label={$t("chat.datesTitle")}
    title={$t("chat.datesTitle")}
  >
    <CalendarDays aria-hidden="true" /><span class="hidden lg:inline" aria-hidden="true">{$t("chat.dates")}</span>
  </Popover.Trigger>
  <Popover.Portal>
    <Popover.Content
      align="end"
      sideOffset={4}
      collisionPadding={8}
      role="dialog"
      aria-label={$t("chat.datesTitle")}
      class={cn(popover, "max-h-[min(60vh,var(--bits-popover-content-available-height))] w-64 overflow-y-auto")}
    >
      <p class={popoverLabel}>{$t("chat.datesTitle")}</p>
      {#if days.isPending}
        <p class="px-2 py-2 text-xs text-muted">{$t("common.loading")}</p>
      {:else if groups.length === 0}
        <p class="px-2 py-2 text-xs text-muted">{$t("chat.datesEmpty")}</p>
      {:else}
        {#key $language}
          {#each groups as group (group.month)}
            <p class={popoverLabel}>{fmtMonth(group.month)}</p>
            <ul>
              {#each group.days as day (day.day)}
                <li>
                  <button class={cn(popoverItem, "justify-between hover:bg-accent focus-visible:bg-accent")} onclick={() => pick(day.firstId)}>
                    <span>{fmtDay(day.day)}</span><span class="text-xs text-muted">{$t("chat.dayCount", { n: day.count })}</span>
                  </button>
                </li>
              {/each}
            </ul>
          {/each}
        {/key}
      {/if}
    </Popover.Content>
  </Popover.Portal>
</Popover.Root>
