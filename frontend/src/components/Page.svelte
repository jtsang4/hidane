<script lang="ts">
  import type { Snippet } from "svelte";
  import { cn } from "../lib/utils.js";
  import Toolbar from "./Toolbar.svelte";

  let {
    title,
    actions,
    children,
    class: className = "",
  }: {
    title: string;
    actions?: Snippet;
    children: Snippet;
    /** For the content column inside the scroller. */
    class?: string;
  } = $props();
</script>

<div class="flex h-full min-h-0 flex-col">
  <Toolbar {title} {actions} />
  <!-- `relative` keeps absolutely positioned descendants inside the scroller. -->
  <div class="relative min-h-0 flex-1 overflow-y-auto overscroll-contain">
    <div class={cn("mx-auto w-full max-w-4xl space-y-3 p-4 sm:p-6", className)}>
      {@render children()}
    </div>
  </div>
</div>
