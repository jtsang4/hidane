<script lang="ts">
  import type { CardState } from "../lib/api.js";
  import { STATE_DOT } from "../lib/board.js";
  import { cn } from "../lib/utils.js";

  /** A task's state as a dot, wherever it is listed; muted for a task on no board. */
  let { state, class: className = "" }: { state: CardState | null; class?: string } = $props();
</script>

<span aria-hidden="true" class={cn("size-1.5 shrink-0 rounded-full", state ? STATE_DOT[state] : "bg-muted", className)}></span>
