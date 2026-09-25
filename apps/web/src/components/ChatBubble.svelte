<script lang="ts">
  import { t } from "../i18n/index.js";
  import { navigate } from "../lib/router.svelte.js";
  import { payloadText } from "../lib/grouping.js";
  import type { HidaneEvent } from "../lib/api.js";
  import { cn } from "../lib/utils.js";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Markdown from "./Markdown.svelte";

  let {
    event,
    ghost = false,
    streaming = false,
    hidden = false,
    anchored = false,
  }: {
    event: HidaneEvent;
    ghost?: boolean;
    streaming?: boolean;
    /** The person hid it after it was loaded; the server masks later copies itself. */
    hidden?: boolean;
    /** Addressable as `#ev-<id>` for jumps; only where the event is shown once per page. */
    anchored?: boolean;
  } = $props();

  let redacted = $derived(hidden || event.payload["redacted"] === true);
</script>

{#if event.kind === "escalation"}
  <div class="flex justify-center">
    <Badge tone="default">
      ↑ {payloadText(event)}
      {#if event.workItemId}
        <a href={`/items/${event.workItemId}`} class="ml-1 underline" onclick={(e) => { e.preventDefault(); navigate(`/items/${event.workItemId}`); }}>{event.workItemId}</a>
      {/if}
    </Badge>
  </div>
{:else if event.kind === "agent.error"}
  <div class="flex justify-center"><Badge tone="danger">{payloadText(event)}</Badge></div>
{:else}
  <div id={anchored ? `ev-${event.id}` : undefined} class={cn("flex", event.kind === "user.message" ? "justify-end" : "justify-start")}>
    <div class={cn("max-w-[85%] rounded-lg px-3 py-2 text-sm break-words", redacted ? "border border-dashed border-border text-muted italic" : event.kind === "user.message" ? "bg-primary text-primary-foreground" : "bg-surface-2", ghost && "opacity-60")}>
      {#if redacted}
        <span>{$t("chat.hidden")}</span>
      {:else if event.kind === "user.message"}
        <span class="whitespace-pre-wrap">{payloadText(event)}</span>
      {:else}
        <Markdown content={payloadText(event)} />
      {/if}
      <div class="mt-1 text-[10px] opacity-60">
        {#if ghost}{$t("pending.sending")}{:else if streaming}{$t("pending.writing")}{:else}<Time iso={event.ts} />{/if}
      </div>
    </div>
  </div>
{/if}
