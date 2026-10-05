<script lang="ts">
  import CircleHelp from "@lucide/svelte/icons/circle-question-mark";
  import { t } from "../i18n/index.js";
  import type { EscalationStep } from "../lib/api.js";
  import Time from "./Time.svelte";

  /** What a task asks the person: the line over it, the question, and what was tried on the way up. */
  let { heading, ts, text, path, listed = false }: {
    heading: string;
    /** When it was asked, after the heading. */
    ts?: string;
    text: string;
    path: readonly EscalationStep[];
    /** In the conversation the steps tried are listed outright; elsewhere they fold away. */
    listed?: boolean;
  } = $props();

  let tried = $derived(path.filter((step) => step.tried));
</script>

{#snippet steps()}
  <ul class="mt-1 list-disc pl-4 text-xs text-muted">
    {#each tried as step (step.workItemId)}<li><span class="text-foreground/80">{step.title}</span> — {step.tried}</li>{/each}
  </ul>
{/snippet}

<p class="flex items-center gap-1.5 text-xs font-medium text-danger">
  <CircleHelp size={13} aria-hidden="true" />{heading}{#if ts}{" · "}<Time iso={ts} />{/if}
</p>
<p class="mt-1 whitespace-pre-wrap select-text">{text}</p>
{#if tried.length > 0 && listed}
  {@render steps()}
{:else if tried.length > 0}
  <details class="mt-1 text-xs text-muted">
    <summary class="cursor-pointer">{$t("task.tried")}</summary>
    {@render steps()}
  </details>
{/if}
