<script lang="ts">
  import { cn } from "../../lib/utils.js";

  let {
    checked,
    onchange,
    label,
    id,
    disabled = false,
  }: {
    checked: boolean;
    onchange: (checked: boolean) => void;
    /** Accessible name; usually the settings row's label. */
    label: string;
    id?: string;
    disabled?: boolean;
  } = $props();
</script>

<button
  {id}
  type="button"
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {disabled}
  class={cn(
    "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:opacity-50",
    checked ? "bg-primary" : "bg-surface-2 ring-1 ring-border ring-inset",
  )}
  onclick={() => onchange(!checked)}
>
  <span class={cn("inline-block h-4 w-4 rounded-full bg-white shadow transition-transform", checked ? "translate-x-[18px]" : "translate-x-0.5")}></span>
</button>
