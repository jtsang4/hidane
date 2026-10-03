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
    "inline-flex size-3.5 shrink-0 items-center justify-center rounded-sm border border-input bg-field text-primary-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary/70 disabled:opacity-50 data-[state=checked]:border-primary data-[state=checked]:bg-primary",
    className,
  )}
>
  {#snippet children({ checked: isChecked })}
    {#if isChecked}<Check size={10} strokeWidth={3} aria-hidden="true" />{/if}
  {/snippet}
</Checkbox.Root>
