<script lang="ts">
  import { X } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { dismissToast, toastStore } from "../lib/toast.js";
  import { cn } from "../lib/utils.js";
</script>

{#if $toastStore.length > 0}
  <!-- Top centre, just under the toolbar row: never over the composer or the phone nav. -->
  <div class="pointer-events-none fixed inset-x-0 top-14 z-[80] flex flex-col items-center gap-2 px-4" aria-live="polite">
    {#each $toastStore as toast (toast.id)}
      <div
        role="alert"
        data-tone={toast.tone}
        class={cn(
          "pointer-events-auto flex w-fit max-w-md animate-pop-in items-start gap-2.5 rounded-lg border px-3 py-2 text-sm shadow-popover backdrop-blur",
          toast.tone === "danger"
            ? "border-danger/40 bg-popover/95 text-danger"
            : "border-border bg-popover/95",
        )}
      >
        <span class="min-w-0 flex-1 break-words">{toast.message}</span>
        <button class="shrink-0 opacity-70 hover:opacity-100" aria-label={$t("common.dismiss")} onclick={() => dismissToast(toast.id)}>
          <X size={14} class="mt-[3px]" />
        </button>
      </div>
    {/each}
  </div>
{/if}
