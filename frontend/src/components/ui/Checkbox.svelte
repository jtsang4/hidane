<script lang="ts">
  import { Checkbox } from "bits-ui";
  import { Check } from "@lucide/svelte";
  import { cn } from "../../lib/utils.js";

  let {
    checked = $bindable(false),
    id,
    disabled = false,
    class: className = "",
  }: {
    checked?: boolean;
    /** Wrap it in a `<label>` or point one at `id`: the label names it. */
    id?: string | undefined;
    disabled?: boolean;
    class?: string;
  } = $props();
</script>

<Checkbox.Root
  bind:checked
  {id}
  {disabled}
  class={cn(
    "inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-border bg-surface text-primary-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:opacity-50 data-[state=checked]:border-primary data-[state=checked]:bg-primary",
    className,
  )}
>
  {#snippet children({ checked: isChecked })}
    {#if isChecked}<Check size={12} strokeWidth={3} aria-hidden="true" />{/if}
  {/snippet}
</Checkbox.Root>
