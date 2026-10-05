<script lang="ts">
  import { Calendar, Popover } from "bits-ui";
  import { parseDate, type DateValue } from "@internationalized/date";
  import CalendarDays from "@lucide/svelte/icons/calendar-days";
  import ChevronLeft from "@lucide/svelte/icons/chevron-left";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import { language, t } from "../../i18n/index.js";
  import { popover, toolbarButton } from "../../lib/styles.js";
  import { cn, dateLocale, fmtDay, fmtShortDay } from "../../lib/utils.js";

  let {
    value,
    onchange,
    label,
    max,
    class: className = "",
  }: {
    /** A local `YYYY-MM-DD` day. */
    value: string;
    onchange: (day: string) => void;
    /** What the field is, read before the day it shows. */
    label: string;
    /** The last day that can be picked, `YYYY-MM-DD`. */
    max?: string | undefined;
    class?: string;
  } = $props();

  const navButton =
    "flex size-6 items-center justify-center rounded-md text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70 disabled:pointer-events-none disabled:opacity-30";

  let open = $state(false);
  let content = $state<HTMLElement | null>(null);
  let date = $derived(parse(value));
  let maxDate = $derived(max === undefined ? undefined : parse(max));
  let locale = $derived(dateLocale($language));
  let text = $derived.by(() => {
    void $language;
    return { full: fmtDay(value), short: fmtShortDay(value) };
  });

  function parse(day: string): DateValue | undefined {
    try {
      return parseDate(day);
    } catch {
      return undefined;
    }
  }
</script>

<Popover.Root bind:open>
  <Popover.Trigger
    aria-label={`${label} ${text.full}`}
    class={cn(
      toolbarButton,
      "text-foreground/85 data-[state=open]:bg-accent",
      className,
    )}
  >
    <CalendarDays size={13} class="shrink-0 text-muted" aria-hidden="true" />
    <span class="hidden sm:inline">{text.full}</span><span class="sm:hidden">{text.short}</span>
  </Popover.Trigger>
  <Popover.Portal>
    <Popover.Content
      sideOffset={4}
      collisionPadding={8}
      align="end"
      role="dialog"
      aria-label={label}
      class={cn(popover, "p-2.5")}
      bind:ref={content}
      onOpenAutoFocus={(event) => {
        // The calendar's own initialFocus does not survive the popover's focus handling: hand focus to its roving day.
        event.preventDefault();
        content?.querySelector<HTMLElement>("[data-bits-day][tabindex='0']")?.focus();
      }}
    >
      <Calendar.Root
        type="single"
        {...date ? { value: date } : {}}
        onValueChange={(next) => {
          if (next === undefined) return;
          open = false;
          onchange(next.toString());
        }}
        {locale}
        calendarLabel={label}
        weekStartsOn={$language === "en" ? 0 : 1}
        weekdayFormat="short"
        {...maxDate ? { maxValue: maxDate } : {}}
        preventDeselect
        fixedWeeks
      >
        {#snippet children({ months, weekdays })}
          <Calendar.Header class="flex items-center justify-between pb-2">
            <!-- bits-ui names these in English and its own props win a merge, so the label goes on after them. -->
            <Calendar.PrevButton>
              {#snippet child({ props })}
                <button {...props} aria-label={$t("common.prevMonth")} class={navButton}><ChevronLeft size={14} aria-hidden="true" /></button>
              {/snippet}
            </Calendar.PrevButton>
            <Calendar.Heading class="text-xs font-medium" />
            <Calendar.NextButton>
              {#snippet child({ props })}
                <button {...props} aria-label={$t("common.nextMonth")} class={navButton}><ChevronRight size={14} aria-hidden="true" /></button>
              {/snippet}
            </Calendar.NextButton>
          </Calendar.Header>
          {#each months as month (month.value.toString())}
            <Calendar.Grid class="border-collapse select-none">
              <Calendar.GridHead>
                <Calendar.GridRow class="flex">
                  {#each weekdays as day, index (index)}
                    <Calendar.HeadCell class="w-7 pb-1 text-2xs font-normal text-muted">{day}</Calendar.HeadCell>
                  {/each}
                </Calendar.GridRow>
              </Calendar.GridHead>
              <Calendar.GridBody>
                {#each month.weeks as week, index (index)}
                  <Calendar.GridRow class="flex">
                    {#each week as day (day.toString())}
                      <Calendar.Cell date={day} month={month.value} class="p-0">
                        <Calendar.Day
                          class="flex size-7 items-center justify-center rounded-md text-xs outline-none hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary/70 data-disabled:pointer-events-none data-disabled:text-muted/40 data-outside-month:text-muted/50 data-selected:bg-primary data-selected:font-medium data-selected:text-primary-foreground data-today:font-semibold data-today:text-primary data-selected:data-today:text-primary-foreground"
                        />
                      </Calendar.Cell>
                    {/each}
                  </Calendar.GridRow>
                {/each}
              </Calendar.GridBody>
            </Calendar.Grid>
          {/each}
        {/snippet}
      </Calendar.Root>
    </Popover.Content>
  </Popover.Portal>
</Popover.Root>
