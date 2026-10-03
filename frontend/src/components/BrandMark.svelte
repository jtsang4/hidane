<script lang="ts">
  /** hidane's ember (the favicon's flame), lit from the top: amber at the tip, deep orange at the base. */
  let { size = 24, class: className = "" }: { size?: number; class?: string } = $props();

  // Several marks can share a page; each needs its own gradient id.
  const id = `ember-${Math.random().toString(36).slice(2, 8)}`;
</script>

<svg width={size} height={size} viewBox="1.5 2 21 21" class={className} aria-hidden="true">
  <defs>
    <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="oklch(0.84 0.15 78)" />
      <stop offset="1" stop-color="oklch(0.66 0.19 42)" />
    </linearGradient>
  </defs>
  <path
    d="M12 3q1 4 4 6.5t3 5.5a1 1 0 0 1-14 0 5 5 0 0 1 1-3 1 1 0 0 0 5 0c0-2-1.5-3-1.5-5q0-2 2.5-4"
    fill={`url(#${id})`}
    stroke={`url(#${id})`}
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
  />
</svg>
