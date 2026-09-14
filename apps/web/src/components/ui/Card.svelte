<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLAttributes<HTMLDivElement> & { children?: Snippet };

  let { children, class: className = "", ...rest }: Props = $props();
</script>

<div {...rest} class={cn("rounded-lg border border-border bg-surface p-4", className)}>
  {@render children?.()}
</div>
