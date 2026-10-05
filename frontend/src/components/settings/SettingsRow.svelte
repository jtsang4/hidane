<script lang="ts">
  import type { Snippet } from "svelte";
  import { cn } from "../../lib/utils.js";

  let {
    label,
    hint = "",
    for: controlId = undefined,
    children,
    below,
    wide = false,
  }: {
    label: string;
    hint?: string;
    /** The control's id, so clicking the label focuses it and it gets the label as its name. */
    for?: string | undefined;
    /** The control, on the right. */
    children?: Snippet;
    /** Full-width content under the row (a warning, a result). */
    below?: Snippet | undefined;
    /** A field rather than a toggle: on a phone it takes the row's full width under the label. */
    wide?: boolean;
  } = $props();
</script>

<div class="px-3.5 py-2.5">
  <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
    <div class="min-w-0 flex-1 basis-48">
      {#if controlId}
        <label for={controlId} class="text-sm">{label}</label>
      {:else}
        <div class="text-sm">{label}</div>
      {/if}
      {#if hint}<p class="mt-0.5 text-xs text-muted">{hint}</p>{/if}
    </div>
    {#if children}<div class={cn("flex max-w-full min-w-0 shrink-0 items-center justify-end gap-2", wide && "max-sm:w-full")}>{@render children()}</div>{/if}
  </div>
  {#if below}<div class="mt-2">{@render below()}</div>{/if}
</div>
