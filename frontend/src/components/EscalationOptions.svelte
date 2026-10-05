<script lang="ts">
  import { t } from "../i18n/index.js";
  import Button from "./ui/Button.svelte";

  let {
    options,
    disabled = false,
    onchoose,
  }: {
    options: readonly string[];
    disabled?: boolean;
    /** Picking one answers the question with its words, like a typed reply. */
    onchoose: (option: string) => void;
  } = $props();
</script>

{#if options.length > 0}
  <div class="flex flex-wrap gap-1.5" role="group" aria-label={$t("task.question")}>
    {#each options as option (option)}
      <Button variant="secondary" size="sm" class="max-w-full" {disabled} aria-label={$t("task.chooseOption", { option })} onclick={() => onchoose(option)}>
        <span class="truncate">{option}</span>
      </Button>
    {/each}
  </div>
{/if}
