<script lang="ts">
  import type { Snippet } from "svelte";

  let { title = "", description = "", children }: { title?: string; description?: string; children: Snippet } = $props();
  const headingId = $props.id();
</script>

<section class="space-y-2" aria-labelledby={title ? headingId : undefined}>
  {#if title}
    <div>
      <h2 id={headingId} class="text-sm font-medium">{title}</h2>
      {#if description}<p class="mt-0.5 text-xs text-muted">{description}</p>{/if}
    </div>
  {/if}
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    {@render children()}
  </div>
</section>
