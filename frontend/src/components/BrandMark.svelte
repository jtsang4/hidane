<script lang="ts">
  /** The ember (the favicon's flame), lit from the top; its colors are the --color-ember-* tokens. */
  let { size = 24, class: className = "" }: { size?: number; class?: string } = $props();

  // Several marks can share a page; each needs its own gradient id.
  const id = $props.id();
</script>

<svg width={size} height={size} viewBox="1.5 2 21 21" class={className} aria-hidden="true">
  <defs>
    <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" style:stop-color="var(--color-ember-tip)" />
      <stop offset="1" style:stop-color="var(--color-ember-base)" />
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
