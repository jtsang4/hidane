<script lang="ts">
  import type { HTMLTextareaAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLTextareaAttributes & { value?: string; ref?: HTMLTextAreaElement | undefined };

  let { value = $bindable(""), ref = $bindable(), class: className = "", ...rest }: Props = $props();
</script>

<textarea
  {...rest}
  bind:this={ref}
  bind:value
  class={cn(
    "w-full resize-none rounded-md border border-border bg-surface px-3 py-2 text-sm placeholder:text-muted focus-visible:outline-2 focus-visible:outline-primary",
    className,
  )}
></textarea>
