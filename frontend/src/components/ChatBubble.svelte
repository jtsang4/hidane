<script lang="ts">
  import { t } from "../i18n/index.js";
  import { escalationText } from "../lib/escalation.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
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
      ↑ {escalationText(event.payload) || payloadText(event)}
      {#if event.workItemId}
        {@const href = focusHref(event.workItemId)}
        <a {href} class="ml-1 underline" onclick={(e) => { e.preventDefault(); navigate(href); }}>{event.workItemId}</a>
      {/if}
    </Badge>
  </div>
{:else if event.kind === "agent.error"}
  <div class="flex justify-center"><Badge tone="danger">{payloadText(event)}</Badge></div>
{:else}
  <div id={anchored ? `ev-${event.id}` : undefined} class={cn("flex", event.kind === "user.message" ? "justify-end" : "justify-start")}>
    <div class={cn("max-w-[85%] rounded-lg px-3 py-1.5 text-sm break-words",
      // Only what arrives while watched rises in; history loads still.
      (ghost || streaming) && "animate-rise-in", redacted ? "border border-dashed border-border text-muted italic" : event.kind === "user.message" ? "bg-linear-to-b from-primary/22 to-primary/14 text-foreground shadow-ember-edge" : "bg-surface-2 shadow-hairline", ghost && "opacity-60")}>
      {#if redacted}
        <span>{$t("chat.hidden")}</span>
      {:else if event.kind === "user.message"}
        <span class="whitespace-pre-wrap select-text">{payloadText(event)}</span>
      {:else}
        <Markdown content={payloadText(event)} {streaming} class="select-text" />
      {/if}
      <div class={cn("mt-0.5 text-2xs", event.kind === "user.message" ? "text-foreground/60" : "text-muted")}>
        {#if ghost}{$t("pending.sending")}{:else if streaming}{$t("pending.writing")}{:else}<Time iso={event.ts} />{/if}
      </div>
    </div>
  </div>
{/if}
