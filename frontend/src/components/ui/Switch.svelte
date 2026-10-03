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
    "relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary/70 disabled:opacity-50",
    checked ? "bg-primary" : "bg-white/15",
  )}
  onclick={() => onchange(!checked)}
>
  <span class={cn("inline-block size-3 rounded-full bg-white shadow-sm transition-transform", checked ? "translate-x-3.5" : "translate-x-0.5")}></span>
</button>
