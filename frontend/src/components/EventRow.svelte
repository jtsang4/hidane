<script lang="ts">
  import { t } from "../i18n/index.js";
  import { navigate } from "../lib/router.svelte.js";
  import type { HidaneEvent } from "../lib/api.js";
  import { cn } from "../lib/utils.js";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";
  import Card from "./ui/Card.svelte";

  let { event }: { event: HidaneEvent } = $props();
  let open = $state(false);
  let copied = $state(false);
  let json = $derived(JSON.stringify(event, null, 2));

  async function copy(): Promise<void> {
    await navigator.clipboard?.writeText(json);
    copied = true;
    window.setTimeout(() => (copied = false), 1500);
  }
</script>

<Card class="cursor-pointer p-3 text-sm" onclick={() => (open = !open)}>
  <div class="flex flex-wrap items-center gap-2">
    <span class="font-mono text-xs text-muted">#{event.seq}</span>
    <Badge tone={event.source.startsWith("agent:") ? "default" : "muted"}>{event.kind}</Badge>
    <span class="text-xs text-muted">{event.source}</span>
    {#if event.workItemId}
      <a href={`/items/${event.workItemId}`} class="font-mono text-xs text-primary underline" onclick={(e) => { e.stopPropagation(); e.preventDefault(); navigate(`/items/${event.workItemId}`); }}>{event.workItemId}</a>
    {/if}
    <span class="ml-auto text-xs text-muted"><Time iso={event.ts} /></span>
  </div>
  {#if open}
    <div class="mt-2 space-y-1">
      <pre class="overflow-x-auto rounded bg-background p-2 text-xs">{json}</pre>
      <Button variant="ghost" size="sm" onclick={(e) => { e.stopPropagation(); void copy(); }}>{copied ? $t("common.copied") : $t("common.copy")}</Button>
    </div>
  {/if}
</Card>
