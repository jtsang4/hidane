<script lang="ts">
  import { t } from "../i18n/index.js";
  import { copyText } from "../lib/native.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
  import type { HidaneEvent } from "../lib/api.js";
  import { cn } from "../lib/utils.js";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";

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

<!-- A row of the event list: the list draws the frame and the dividers. -->
<div class="px-3 py-2 text-sm">
  <div class="flex flex-wrap items-center gap-2">
    <button class="flex items-center gap-2 rounded-sm text-left focus-visible:outline-2 focus-visible:outline-primary/70" aria-expanded={open} onclick={() => (open = !open)}>
      <span class="w-9 font-mono text-xs text-muted tabular-nums">#{event.seq}</span>
      <Badge tone="muted" class="font-mono text-foreground/85">{event.kind}</Badge>
      <span class="text-xs text-muted">{event.source}</span>
    </button>
    {#if event.workItemId}
      {@const href = focusHref(event.workItemId)}
      <a {href} class="font-mono text-xs text-foreground/80 underline-offset-2 hover:text-primary hover:underline" onclick={(e) => { e.preventDefault(); navigate(href); }}>{event.workItemId}</a>
    {/if}
    <span class="ml-auto text-xs text-muted"><Time iso={event.ts} /></span>
  </div>
  {#if open}
    <div class="mt-2 space-y-1">
      <pre class="overflow-x-auto rounded-sm bg-background p-2 text-xs select-text">{json}</pre>
      <Button variant="ghost" size="sm" onclick={() => void copy()}>{copied ? $t("common.copied") : $t("common.copy")}</Button>
    </div>
  {/if}
</div>
