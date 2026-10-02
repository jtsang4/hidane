<script lang="ts">
  import { t } from "../i18n/index.js";
  import { copyText } from "../lib/native.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
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
    try {
      await copyText(json);
    } catch {
      return;
    }
    copied = true;
    window.setTimeout(() => (copied = false), 1500);
  }
</script>

<Card class="p-3 text-sm">
  <div class="flex flex-wrap items-center gap-2">
    <button class="flex items-center gap-2 rounded text-left focus-visible:outline-2 focus-visible:outline-primary" aria-expanded={open} onclick={() => (open = !open)}>
      <span class="font-mono text-xs text-muted">#{event.seq}</span>
      <Badge tone={event.source.startsWith("agent:") ? "default" : "muted"}>{event.kind}</Badge>
      <span class="text-xs text-muted">{event.source}</span>
    </button>
    {#if event.workItemId}
      {@const href = focusHref(event.workItemId)}
      <a {href} class="font-mono text-xs text-primary underline" onclick={(e) => { e.preventDefault(); navigate(href); }}>{event.workItemId}</a>
    {/if}
    <span class="ml-auto text-xs text-muted"><Time iso={event.ts} /></span>
  </div>
  {#if open}
    <div class="mt-2 space-y-1">
      <pre class="overflow-x-auto rounded bg-background p-2 text-xs select-text">{json}</pre>
      <Button variant="ghost" size="sm" onclick={() => void copy()}>{copied ? $t("common.copied") : $t("common.copy")}</Button>
    </div>
  {/if}
</Card>
