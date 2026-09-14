<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLButtonAttributes } from "svelte/elements";
  import { cva, type VariantProps } from "class-variance-authority";
  import { cn } from "../../lib/utils.js";

  const buttonVariants = cva(
    "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md text-sm font-medium whitespace-nowrap transition-colors disabled:opacity-50 disabled:pointer-events-none focus-visible:outline-2 focus-visible:outline-primary",
    {
      variants: {
        variant: {
          default: "bg-primary text-primary-foreground hover:bg-primary/90",
          outline: "border border-border bg-transparent hover:bg-surface-2",
          ghost: "hover:bg-surface-2",
        },
        size: {
          default: "h-9 px-4",
          sm: "h-8 px-3 text-xs",
          icon: "h-9 w-9",
        },
      },
      defaultVariants: { variant: "default", size: "default" },
    },
  );

  type Props = HTMLButtonAttributes & {
    children?: Snippet;
    variant?: VariantProps<typeof buttonVariants>["variant"];
    size?: VariantProps<typeof buttonVariants>["size"];
  };

  let {
    children,
    class: className = "",
    variant = "default",
    size = "default",
    ...rest
  }: Props = $props();
</script>

<button {...rest} class={cn(buttonVariants({ variant, size }), className)}>
  {@render children?.()}
</button>
