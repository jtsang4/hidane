<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLInputAttributes & { value?: string; ref?: HTMLInputElement | null };

  let { value = $bindable(""), ref = $bindable(null), class: className = "", ...rest }: Props = $props();
</script>

<input
  bind:this={ref}
  {...rest}
  bind:value
  class={cn(
    "h-9 w-full rounded-md border border-border bg-surface px-3 text-sm placeholder:text-muted focus-visible:outline-2 focus-visible:outline-primary",
    className,
  )}
  />
