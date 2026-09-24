<script lang="ts">
  import { X } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { digest, type Notice } from "../lib/notices.js";

  let {
    notices,
    titleOf,
    onjump,
    ondismiss,
  }: {
    notices: Notice[];
    titleOf: (id: string) => string;
    onjump: (notice: Notice) => void;
    ondismiss: () => void;
  } = $props();

  let view = $derived(digest(notices));

  function label(notice: Notice): string {
    if (notice.kind === "choice") return $t("notice.choice");
    if (!notice.workItemId) return $t("notice.main");
    return $t(notice.kind === "question" ? "notice.question" : "notice.reply", { title: titleOf(notice.workItemId) });
  }
</script>

{#if notices.length > 0}
  <div class="pointer-events-none absolute inset-x-0 bottom-2 z-10 flex justify-center px-3" role="status" aria-live="polite">
    <div class="pointer-events-auto flex max-w-full flex-col gap-1 rounded-lg border border-border bg-surface/95 p-1.5 text-xs shadow-lg backdrop-blur">
      {#each view.shown as notice (notice.root)}
        <button class="flex items-center gap-2 rounded px-2 py-1 text-left hover:bg-surface-2" onclick={() => onjump(notice)}>
          <span aria-hidden="true" class={notice.kind === "reply" ? "h-1.5 w-1.5 rounded-full bg-primary" : "h-1.5 w-1.5 rounded-full bg-danger"}></span>
          <span class="truncate">{label(notice)}</span>
          <span class="ml-auto shrink-0 text-primary">{$t("notice.jump")}</span>
        </button>
      {/each}
      <div class="flex items-center gap-2 px-2 pt-0.5 text-muted">
        {#if view.hidden > 0}<span>{$t("notice.more", { n: view.hidden })}</span>{/if}
        <button class="ml-auto flex items-center gap-1 hover:text-foreground" onclick={ondismiss}><X size={12} />{$t("notice.dismissAll")}</button>
      </div>
    </div>
  </div>
{/if}
