<script lang="ts">
  import type { Snippet } from "svelte";
  import type { HTMLButtonAttributes } from "svelte/elements";
  import { cva, type VariantProps } from "class-variance-authority";
  import { cn } from "../../lib/utils.js";

  // `default` (solid primary) is for the one action a view is about: send, create, answer.
  const buttonVariants = cva(
    "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md text-sm whitespace-nowrap transition duration-150 active:scale-[0.97] disabled:pointer-events-none disabled:opacity-40 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-primary/70 coarse:min-h-9",
    {
      variants: {
        variant: {
          // Dimmed with opacity, a primary turns brown on the dark theme: disabled, it is drawn as its own outline.
          default: "bg-primary bg-linear-to-b from-sheen to-transparent font-medium text-primary-foreground shadow-primary hover:bg-primary/92 disabled:bg-transparent disabled:bg-none disabled:text-primary/55 disabled:opacity-100 disabled:shadow-primary-outline",
          soft: "bg-primary/12 text-primary hover:bg-primary/20",
          danger: "bg-danger bg-linear-to-b from-sheen to-transparent font-medium text-danger-foreground shadow-danger hover:bg-danger/90",
          secondary: "bg-accent text-foreground hover:bg-accent-strong",
          ghost: "text-foreground/85 hover:bg-accent hover:text-foreground",
        },
        size: {
          default: "h-7 px-2.5 [&_svg]:size-3.5",
          sm: "h-6 gap-1 px-2 text-xs [&_svg]:size-3.5",
          lg: "h-8 px-3 [&_svg]:size-4",
          icon: "size-7 coarse:min-w-9 [&_svg]:size-4",
          "icon-sm": "size-6 coarse:min-w-9 [&_svg]:size-3.5",
        },
      },
      compoundVariants: [{ variant: "ghost", size: ["icon", "icon-sm"], class: "text-muted" }],
      defaultVariants: { variant: "default", size: "default" },
    },
  );

  type Props = HTMLButtonAttributes & {
    children?: Snippet;
    ref?: HTMLButtonElement | null;
    variant?: VariantProps<typeof buttonVariants>["variant"];
    size?: VariantProps<typeof buttonVariants>["size"];
  };

  let {
    children,
    ref = $bindable(null),
    class: className = "",
    variant = "default",
    size = "default",
    ...rest
  }: Props = $props();
</script>

<button bind:this={ref} {...rest} class={cn(buttonVariants({ variant, size }), className)}>
  {@render children?.()}
</button>
