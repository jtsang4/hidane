<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Tone = "default" | "success" | "danger" | "muted";
  type Props = HTMLAttributes<HTMLSpanElement> & { children?: Snippet; tone?: Tone };

  let { children, class: className = "", tone = "default", ...rest }: Props = $props();
  const tones: Record<Tone, string> = {
    default: "bg-primary/15 text-primary",
    success: "bg-success/15 text-success",
    danger: "bg-danger/15 text-danger",
    muted: "bg-surface-2 text-muted",
  };
</script>

<span
  {...rest}
  class={cn("inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium", tones[tone], className)}
>
  {@render children?.()}
</span>
