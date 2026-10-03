<script lang="ts">
  /**
   * A file path that wraps only after a "/", never in the middle of a name.
   * `<wbr>` adds no characters, so a selected path copies unchanged.
   * Put it in an element with `break-words`, not `break-all`.
   */
  let { path }: { path: string } = $props();

  let parts = $derived(path.split(/(?<=\/)/));
</script>

{#each parts as part, index (index)}{part}{#if index < parts.length - 1}<wbr />{/if}{/each}
