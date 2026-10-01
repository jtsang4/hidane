<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLSelectAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLSelectAttributes & { value?: string; children?: Snippet };

  let { value = $bindable(""), class: className = "", children, ...rest }: Props = $props();
</script>

<select
  {...rest}
  bind:value
  class={cn(
    "h-9 w-full rounded-md border border-border bg-surface px-2 text-sm focus-visible:outline-2 focus-visible:outline-primary",
    className,
  )}
>
  {@render children?.()}
</select>
