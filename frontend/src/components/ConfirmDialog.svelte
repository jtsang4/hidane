<script lang="ts">
  import { t } from "../i18n/index.js";
  import { trapFocus } from "../lib/focusTrap.js";
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

  const titleId = `confirm-title-${Math.random().toString(36).slice(2, 8)}`;
  const bodyId = `${titleId}-body`;

  function onkeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onresult(false);
    } else if (event.key === "Enter" && !event.isComposing && !(event.target instanceof HTMLButtonElement)) {
      // On a focused button Enter already clicks it — including Cancel.
      event.preventDefault();
      onresult(true);
    }
  }
</script>

<div class="fixed inset-0 z-[70] flex items-center justify-center bg-black/50 p-4" role="presentation" onmousedown={(event) => { if (event.target === event.currentTarget) onresult(false); }}>
  <div
    role="alertdialog"
    aria-modal="true"
    aria-labelledby={titleId}
    aria-describedby={body ? bodyId : undefined}
    tabindex="-1"
    class="w-full max-w-sm rounded-xl border border-border bg-surface p-5 shadow-2xl outline-none"
    {onkeydown}
    {@attach trapFocus()}
  >
    <h2 id={titleId} class="text-sm font-semibold">{title}</h2>
    {#if body}<p id={bodyId} class="mt-2 text-sm whitespace-pre-line text-muted">{body}</p>{/if}
    <div class="mt-5 flex justify-end gap-2">
      <Button variant="outline" size="sm" onclick={() => onresult(false)}>{cancelLabel || $t("common.cancel")}</Button>
      <Button variant={destructive ? "danger" : "default"} size="sm" data-autofocus onclick={() => onresult(true)}>{confirmLabel || $t("common.confirm")}</Button>
    </div>
  </div>
</div>
