<script lang="ts">
  import { Ellipsis } from "@lucide/svelte";
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
  class={cn("flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted hover:bg-surface-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary", className)}
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
