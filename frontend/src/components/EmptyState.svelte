<script lang="ts">
  import type { Snippet } from "svelte";
  import type { Flame } from "@lucide/svelte";
  import { cn } from "../lib/utils.js";
  import BrandMark from "./BrandMark.svelte";

  let {
    icon: Icon,
    text,
    ember = false,
    children,
    class: className = "",
  }: {
    /** Left out where `ember` shows the brand mark instead. */
    icon?: typeof Flame | undefined;
    /** One sentence: what is missing and, if it helps, how it comes to exist. */
    text: string;
    /** The brand glow, for the few places that greet a person (the empty conversation). */
    ember?: boolean;
    /** The action that fills the emptiness, if there is one. */
    children?: Snippet;
    class?: string;
  } = $props();
</script>

<div class={cn("flex animate-rise-in flex-col items-center px-6 py-16 text-center", className)}>
  <div class="relative mb-5" aria-hidden="true">
    {#if ember}
      <div class="absolute -inset-10 rounded-full bg-primary/20 blur-2xl"></div>
      <div class="absolute -inset-3 rounded-full bg-primary/10 blur-md"></div>
    {/if}
    <div class="relative grid size-12 place-items-center rounded-2xl border border-border bg-linear-to-b from-surface-2 to-surface shadow-popover">
      {#if ember}<BrandMark size={22} />{:else if Icon}<Icon size={22} class="text-muted" />{/if}
    </div>
  </div>
  <p class="max-w-sm text-sm text-foreground/80">{text}</p>
  {#if children}<div class="mt-4 flex items-center gap-2">{@render children()}</div>{/if}
</div>
