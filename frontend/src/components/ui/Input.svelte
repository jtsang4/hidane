<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";
  import { field, fieldSize } from "../../lib/styles.js";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLInputAttributes & { value?: string; ref?: HTMLInputElement | null };

  let { value = $bindable(""), ref = $bindable(null), class: className = "", ...rest }: Props = $props();
</script>

<input
  bind:this={ref}
  {...rest}
  bind:value
  class={cn("block w-full", field, fieldSize.default, className)}
  />
