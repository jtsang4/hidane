<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLAttributes } from "svelte/elements";
  import { cn } from "../../lib/utils.js";

  type Tone = "default" | "success" | "danger" | "muted";
  type Props = HTMLAttributes<HTMLSpanElement> & { children?: Snippet; tone?: Tone };

  let { children, class: className = "", tone = "default", ...rest }: Props = $props();
  const tones: Record<Tone, string> = {
    default: "bg-primary/12 text-primary",
    success: "bg-success/12 text-success",
    danger: "bg-danger/12 text-danger",
    muted: "bg-accent text-muted",
  };
</script>

<span
  {...rest}
  class={cn("inline-flex h-[18px] items-center gap-1 rounded px-1.5 text-2xs font-medium whitespace-nowrap", tones[tone], className)}
>
  {@render children?.()}
</span>
