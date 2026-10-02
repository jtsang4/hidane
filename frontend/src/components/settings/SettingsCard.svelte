<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Props = HTMLAttributes<HTMLElement> & {
    title?: string;
    description?: string;
    /** Controls beside the title, e.g. a Test or Add button. */
    actions?: Snippet;
    children: Snippet;
  };

  let { title = "", description = "", actions, children, class: className = "", ...rest }: Props = $props();
  const headingId = `card-${Math.random().toString(36).slice(2, 8)}`;
</script>

<section {...rest} class={cn("space-y-2", className)} aria-labelledby={title ? headingId : undefined}>
  {#if title || actions}
    <div class="flex items-end gap-3 px-1">
      <div class="min-w-0 flex-1">
        {#if title}<h2 id={headingId} class="text-[13px] font-semibold">{title}</h2>{/if}
        {#if description}<p class="mt-0.5 text-xs text-muted">{description}</p>{/if}
      </div>
      {#if actions}<div class="flex shrink-0 items-center gap-2">{@render actions()}</div>{/if}
    </div>
  {/if}
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    {@render children()}
  </div>
</section>
