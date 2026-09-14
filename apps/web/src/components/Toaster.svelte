<script lang="ts">
  import { X } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { dismissToast, toastStore } from "../lib/toast.js";
  import { cn } from "../lib/utils.js";
</script>

{#if $toastStore.length > 0}
  <div class="pointer-events-none fixed inset-x-0 bottom-24 z-50 flex flex-col items-center gap-2 px-4 sm:bottom-4">
    {#each $toastStore as toast (toast.id)}
      <div
        role="alert"
        class={cn(
          "pointer-events-auto flex w-full max-w-md items-start gap-3 rounded-lg border px-3 py-2 text-sm shadow-lg",
          toast.tone === "danger"
            ? "border-danger/40 bg-danger/10 text-danger"
            : "border-border bg-surface",
        )}
      >
        <span class="min-w-0 flex-1 break-words">{toast.message}</span>
        <button class="shrink-0 opacity-70 hover:opacity-100" aria-label={$t("common.dismiss")} onclick={() => dismissToast(toast.id)}>
          <X size={16} />
        </button>
      </div>
    {/each}
  </div>
{/if}
