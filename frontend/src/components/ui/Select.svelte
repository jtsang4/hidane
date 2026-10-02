<script lang="ts" module>
  export interface SelectOption {
    value: string;
    label: string;
    disabled?: boolean;
  }
</script>

<script lang="ts">
  import { Select } from "bits-ui";
  import { Check, ChevronsUpDown } from "@lucide/svelte";
  import { cn } from "../../lib/utils.js";

  let {
    value,
    options,
    onchange,
    id,
    label,
    placeholder = "",
    size = "default",
    disabled = false,
    class: className = "",
  }: {
    value: string;
    options: readonly SelectOption[];
    onchange: (value: string) => void;
    /** For a `<label for>`; the trigger is a button, which a label names and clicks. */
    id?: string | undefined;
    /** Accessible name when no `<label for>` points at the trigger. */
    label?: string | undefined;
    /** Shown while `value` matches no option. */
    placeholder?: string;
    size?: "default" | "sm";
    disabled?: boolean;
    class?: string;
  } = $props();

  let selected = $derived(options.find((option) => option.value === value));
</script>

<Select.Root
  type="single"
  bind:value={() => value, (next) => { if (next !== value) onchange(next); }}
  items={options as SelectOption[]}
  {disabled}
>
  <!-- A select-only combobox (WAI-ARIA): bits-ui renders a plain button, which cannot carry aria-activedescendant. -->
  <Select.Trigger
    {id}
    role="combobox"
    aria-label={label}
    class={cn(
      "inline-flex w-full min-w-0 items-center justify-between gap-1.5 rounded-md border border-border bg-surface text-left hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-50 data-[state=open]:bg-surface-2",
      size === "sm" ? "h-7 px-2 text-xs" : "h-9 px-2.5 text-sm",
      className,
    )}
  >
    <span class={cn("truncate", !selected && "text-muted")}>{selected?.label ?? placeholder}</span>
    <ChevronsUpDown size={size === "sm" ? 12 : 14} class="shrink-0 text-muted" aria-hidden="true" />
  </Select.Trigger>
  <Select.Portal>
    <Select.Content
      sideOffset={4}
      collisionPadding={8}
      class="z-[65] max-h-[min(20rem,var(--bits-select-content-available-height))] max-w-[min(24rem,var(--bits-select-content-available-width))] min-w-[var(--bits-select-anchor-width)] overflow-y-auto rounded-lg border border-border bg-surface p-1 shadow-xl outline-none"
    >
      {#each options as option (option.value)}
        <Select.Item
          value={option.value}
          label={option.label}
          disabled={option.disabled ?? false}
          class={cn(
            "flex items-center gap-2 rounded-md py-1.5 pr-2 pl-1.5 outline-none data-disabled:opacity-50 data-highlighted:bg-surface-2",
            size === "sm" ? "text-xs" : "text-sm",
          )}
        >
          {#snippet children({ selected: isSelected })}
            <Check size={14} class={cn("shrink-0 text-primary", !isSelected && "invisible")} aria-hidden="true" />
            <span class="truncate">{option.label}</span>
          {/snippet}
        </Select.Item>
      {/each}
    </Select.Content>
  </Select.Portal>
</Select.Root>
