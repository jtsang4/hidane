<script lang="ts">
  import { t } from "../i18n/index.js";
  import { navigate } from "../lib/router.svelte.js";
  import { payloadText } from "../lib/grouping.js";
  import type { HidaneEvent } from "../lib/api.js";
  import { cn } from "../lib/utils.js";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Markdown from "./Markdown.svelte";

  let { event, ghost = false }: { event: HidaneEvent; ghost?: boolean } = $props();
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
  <div class={cn("flex", event.kind === "user.message" ? "justify-end" : "justify-start")}>
    <div class={cn("max-w-[85%] rounded-lg px-3 py-2 text-sm break-words", event.kind === "user.message" ? "bg-primary text-primary-foreground" : "bg-surface-2", ghost && "opacity-60")}>
      {#if event.kind === "user.message"}
        <span class="whitespace-pre-wrap">{payloadText(event)}</span>
      {:else}
        <Markdown content={payloadText(event)} />
      {/if}
      <div class="mt-1 text-[10px] opacity-60">
        {#if ghost}{$t("pending.sending")}{:else}<Time iso={event.ts} />{/if}
      </div>
    </div>
  </div>
{/if}
