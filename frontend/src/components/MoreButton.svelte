<script lang="ts">
  import Ellipsis from "@lucide/svelte/icons/ellipsis";
  import { belowElement, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { cn } from "../lib/utils.js";

  let {
    label,
    onopen,
    class: className = "",
  }: {
    label: string;
    /** Opens the same menu the element's right click opens, placed under this button. */
    onopen: (placement: MenuPlacement) => void;
    class?: string;
  } = $props();
</script>

<button
  type="button"
  class={cn("flex size-6 shrink-0 items-center justify-center rounded-md text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70 coarse:size-9", className)}
  aria-label={label}
  title={label}
  aria-haspopup="menu"
  onclick={(event) => {
    event.stopPropagation();
    onopen(belowElement(event.currentTarget));
  }}
>
  <Ellipsis size={14} aria-hidden="true" />
</button>
