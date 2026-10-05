<script lang="ts">
  import type { Snippet } from "svelte";
  import Toolbar from "./Toolbar.svelte";

  let { title, actions, children }: { title: string; actions?: Snippet; children: Snippet } = $props();
</script>

<div class="flex h-full min-h-0 flex-col">
  <Toolbar {title} {actions} />
  <!-- `relative` keeps absolutely positioned descendants inside the scroller. -->
  <div class="relative min-h-0 flex-1 overflow-y-auto overscroll-contain">
    <div class="mx-auto w-full max-w-4xl space-y-3 p-4 sm:p-6">
      {@render children()}
    </div>
  </div>
</div>
