<script lang="ts">
  import { AlertDialog } from "bits-ui";
  import { t } from "../i18n/index.js";
  import { sheetHandle, sheetOnPhone } from "../lib/styles.js";
  import { cn } from "../lib/utils.js";
  import Button from "./ui/Button.svelte";

  let {
    title,
    body = "",
    confirmLabel = "",
    cancelLabel = "",
    destructive = false,
    onresult,
  }: {
    title: string;
    body?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    destructive?: boolean;
    onresult: (confirmed: boolean) => void;
  } = $props();

  let confirmButton = $state<HTMLButtonElement | null>(null);
</script>

<!-- Mounted per request by ConfirmHost: open for as long as it exists; any way of closing it is a "no". -->
<AlertDialog.Root bind:open={() => true, (open) => { if (!open) onresult(false); }}>
  <AlertDialog.Portal>
    <AlertDialog.Overlay class="fixed inset-0 z-[70] animate-fade-in bg-overlay backdrop-blur-[2px]" />
    <AlertDialog.Content
      class={cn("fixed top-1/2 left-1/2 z-[70] w-[calc(100%-2rem)] max-w-sm -translate-x-1/2 -translate-y-1/2 animate-dialog-in rounded-xl border border-border bg-popover p-4 shadow-dialog outline-none", sheetOnPhone)}
      interactOutsideBehavior="close"
      onOpenAutoFocus={(event) => {
        event.preventDefault();
        confirmButton?.focus();
      }}
      onkeydown={(event: KeyboardEvent) => {
        // On a focused button Enter already clicks it — including Cancel.
        if (event.key === "Enter" && !event.isComposing && !(event.target instanceof HTMLButtonElement)) {
          event.preventDefault();
          onresult(true);
        }
      }}
    >
      <div class={cn(sheetHandle, "sm:hidden")} aria-hidden="true"></div>
      <AlertDialog.Title class="text-sm font-semibold">{title}</AlertDialog.Title>
      {#if body}<AlertDialog.Description class="mt-2 space-y-2 text-sm text-muted">{#each body.split(/\n+/) as paragraph, index (index)}<p>{paragraph}</p>{/each}</AlertDialog.Description>{/if}
      <div class="mt-4 flex justify-end gap-2">
        <AlertDialog.Cancel>
          {#snippet child({ props })}
            <Button {...props} variant="secondary">{cancelLabel || $t("common.cancel")}</Button>
          {/snippet}
        </AlertDialog.Cancel>
        <AlertDialog.Action>
          {#snippet child({ props })}
            <Button {...props} bind:ref={confirmButton} variant={destructive ? "danger" : "default"} onclick={() => onresult(true)}>{confirmLabel || $t("common.confirm")}</Button>
          {/snippet}
        </AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog.Portal>
</AlertDialog.Root>
