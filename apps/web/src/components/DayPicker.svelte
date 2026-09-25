<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { CalendarDays } from "@lucide/svelte";
  import { language, t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { daysByMonth } from "../lib/history.js";
  import { cn, fmtDay, fmtMonth } from "../lib/utils.js";

  let { onpick }: { onpick: (eventId: string) => void } = $props();

  let open = $state(false);
  let root = $state<HTMLDivElement | undefined>();
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

  function onWindowClick(event: MouseEvent): void {
    if (open && root && event.target instanceof Node && !root.contains(event.target)) open = false;
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={(event) => { if (event.key === "Escape") open = false; }} />

<div bind:this={root} class="relative shrink-0">
  <button
    class={cn("flex h-9 items-center gap-1.5 whitespace-nowrap rounded-md border border-border px-2.5 text-xs text-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary", open && "bg-surface-2 text-foreground")}
    aria-expanded={open}
    aria-haspopup="true"
    aria-label={$t("chat.datesTitle")}
    title={$t("chat.datesTitle")}
    onclick={() => (open = !open)}
  >
    <CalendarDays size={14} aria-hidden="true" /><span class="hidden sm:inline" aria-hidden="true">{$t("chat.dates")}</span>
  </button>
  {#if open}
    <div class="absolute right-0 z-20 mt-1 max-h-[60vh] w-64 overflow-y-auto rounded-lg border border-border bg-surface p-2 shadow-lg" role="dialog" aria-label={$t("chat.datesTitle")}>
      <p class="px-1 pb-1 text-xs font-medium text-muted">{$t("chat.datesTitle")}</p>
      {#if days.isPending}
        <p class="px-1 py-2 text-xs text-muted">{$t("common.loading")}</p>
      {:else if groups.length === 0}
        <p class="px-1 py-2 text-xs text-muted">{$t("chat.datesEmpty")}</p>
      {:else}
        {#key $language}
          {#each groups as group (group.month)}
            <p class="px-1 pt-2 text-[11px] font-medium text-muted">{fmtMonth(group.month)}</p>
            <ul>
              {#each group.days as day (day.day)}
                <li>
                  <button class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-sm hover:bg-surface-2" onclick={() => pick(day.firstId)}>
                    <span>{fmtDay(day.day)}</span><span class="text-xs text-muted">{$t("chat.dayCount", { n: day.count })}</span>
                  </button>
                </li>
              {/each}
            </ul>
          {/each}
        {/key}
      {/if}
    </div>
  {/if}
</div>
