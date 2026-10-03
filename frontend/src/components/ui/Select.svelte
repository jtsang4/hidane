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
  import { field, fieldSize, popover, popoverItem } from "../../lib/styles.js";
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
      "inline-flex w-full min-w-0 items-center justify-between gap-1.5 text-left data-[state=open]:border-primary/60",
      field,
      fieldSize[size],
      className,
    )}
  >
    <span class={cn("truncate", !selected && "text-muted")}>{selected?.label ?? placeholder}</span>
    <ChevronsUpDown size={12} class="shrink-0 text-muted" aria-hidden="true" />
  </Select.Trigger>
  <Select.Portal>
    <Select.Content
      sideOffset={4}
      collisionPadding={8}
      class={cn(popover, "max-h-[min(20rem,var(--bits-select-content-available-height))] max-w-[min(24rem,var(--bits-select-content-available-width))] min-w-[var(--bits-select-anchor-width)] overflow-y-auto")}
    >
      {#each options as option (option.value)}
        <Select.Item
          value={option.value}
          label={option.label}
          disabled={option.disabled ?? false}
          class={cn(popoverItem, "pl-1.5", size === "sm" && "text-xs")}
        >
          {#snippet children({ selected: isSelected })}
            <Check size={13} class={cn("shrink-0 text-primary", !isSelected && "invisible")} aria-hidden="true" />
            <span class="truncate">{option.label}</span>
          {/snippet}
        </Select.Item>
      {/each}
    </Select.Content>
  </Select.Portal>
</Select.Root>
