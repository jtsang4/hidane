<script lang="ts">
  import { confirmState, settleConfirm } from "../lib/confirm.svelte.js";
  import ConfirmDialog from "./ConfirmDialog.svelte";
</script>

{#if confirmState.current}
  {@const request = confirmState.current}
  {#key request.id}
    <ConfirmDialog
      title={request.title}
      body={request.body}
      confirmLabel={request.confirmLabel}
      cancelLabel={request.cancelLabel}
      destructive={request.destructive}
      onresult={settleConfirm}
    />
  {/key}
{/if}
