<script lang="ts">
  import type { HTMLTextareaAttributes } from "svelte/elements";
  import { field } from "../../lib/styles.js";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLTextareaAttributes & { value?: string; ref?: HTMLTextAreaElement | undefined };

  let { value = $bindable(""), ref = $bindable(), class: className = "", ...rest }: Props = $props();
</script>

<textarea
  {...rest}
  bind:this={ref}
  bind:value
  class={cn("block w-full resize-none px-2.5 py-1.5 text-sm", field, className)}
></textarea>
