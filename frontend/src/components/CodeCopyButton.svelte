<script lang="ts">
  import { Check, Copy } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { copyText } from "../lib/native.js";
  import { toastError } from "../lib/toast.js";
  import { cn } from "../lib/utils.js";
  import Button from "./ui/Button.svelte";

  /** Mounted by `Markdown` into the frame around each rendered code block. */
  let { text }: { text: string } = $props();
  let copied = $state(false);
  let timer: number | undefined;

  async function copy(): Promise<void> {
    try {
      await copyText(text);
    } catch (error) {
      toastError(error);
      return;
    }
    copied = true;
    window.clearTimeout(timer);
    timer = window.setTimeout(() => (copied = false), 1500);
  }
</script>

<!-- Opaque, so the code it sits over does not show through; it appears with the pointer or focus, and stays on a touch screen, which has no hover. -->
<Button
  variant="ghost"
  size={copied ? "sm" : "icon-sm"}
  class={cn(
    "absolute top-1 right-1 bg-popover shadow-hairline select-none hover:bg-surface-2",
    copied ? "text-success hover:text-success" : "opacity-0 group-hover/code:opacity-100 focus-visible:opacity-100 coarse:opacity-100",
  )}
  aria-label={copied ? $t("common.copied") : $t("common.copyCode")}
  title={copied ? $t("common.copied") : $t("common.copyCode")}
  onclick={() => void copy()}
>
  {#if copied}<Check aria-hidden="true" /><span>{$t("common.copied")}</span>{:else}<Copy aria-hidden="true" />{/if}
</Button>
<span class="sr-only" aria-live="polite">{copied ? $t("common.copied") : ""}</span>
