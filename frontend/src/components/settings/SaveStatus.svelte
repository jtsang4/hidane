<script lang="ts">
  import Check from "@lucide/svelte/icons/check";
  import LoaderCircle from "@lucide/svelte/icons/loader-circle";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import { t } from "../../i18n/index.js";

  /** Inline state of an auto-saved setting. */
  let { status, error }: { status: "idle" | "saving" | "saved" | "error" | "unsaved"; error: string } = $props();
</script>

<span class="inline-flex min-w-0 items-center gap-1 text-xs" role="status" aria-live="polite">
  {#if status === "saving"}
    <LoaderCircle size={12} class="animate-spin text-muted" aria-hidden="true" /><span class="text-muted">{$t("settings.save.saving")}</span>
  {:else if status === "saved"}
    <Check size={12} class="text-success" aria-hidden="true" /><span class="text-success">{$t("settings.save.saved")}</span>
  {:else if status === "unsaved"}
    <span class="text-muted">{$t("settings.save.unsaved")}</span>
  {:else if status === "error"}
    <TriangleAlert size={12} class="shrink-0 text-danger" aria-hidden="true" /><span class="break-words text-danger">{$t("settings.save.failed", { message: error })}</span>
  {/if}
</span>
