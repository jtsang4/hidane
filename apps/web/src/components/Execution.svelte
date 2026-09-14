<script lang="ts">
  import { ChevronDown, ChevronRight } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import type { HidaneEvent } from "../lib/api.js";
  import type { ExecutionGroup } from "../lib/grouping.js";
  import { cn } from "../lib/utils.js";
  import Time from "./Time.svelte";
  import Markdown from "./Markdown.svelte";
  import Badge from "./ui/Badge.svelte";
  import Card from "./ui/Card.svelte";

  let { group }: { group: ExecutionGroup } = $props();
  let open = $state(false);
  $effect(() => {
    if (group.ok === null) open = true;
  });
  let tone = $derived<"muted" | "success" | "danger">(group.ok === null ? "muted" : group.ok ? "success" : "danger");
  let label = $derived(group.ok === null ? $t("item.running") : group.ok ? $t("item.success") : $t("item.failed"));
</script>

<Card class="p-3">
  <button class="flex w-full items-center gap-2 text-left text-sm" aria-expanded={open} onclick={() => (open = !open)}>
    {#if open}<ChevronDown size={16} />{:else}<ChevronRight size={16} />{/if}
    <span class="font-mono text-xs">{group.executionId}</span>
    <Badge {tone}>{label}</Badge>
    <span class="ml-auto text-xs text-muted">
      {#if group.started}<Time iso={group.started.ts} />{/if}
      {#if group.sideEffects.length > 0} · {$t("item.toolCalls", { n: Math.floor(group.sideEffects.length / 2) })}{/if}
    </span>
  </button>
  {#if open}
    <div class="mt-2 space-y-1 border-t border-border pt-2 text-xs">
      {#if group.started}<p class="whitespace-pre-wrap text-muted">{$t("item.instructions", { text: String(group.started.payload["instructions"] ?? "") })}</p>{/if}
      {#each group.sideEffects as event (event.id)}
        <div class="flex gap-2 font-mono">
          <span class={cn(event.kind.endsWith("intent") ? "text-primary" : "text-success")}>{event.kind.endsWith("intent") ? "→" : "←"}</span>
          <span>{String(event.payload["tool"] ?? "")}</span>
          <span class="truncate text-muted">{event.kind.endsWith("intent") ? String(event.payload["input"] ?? "").slice(0, 120) : event.payload["isError"] === true ? $t("item.sideEffectError") : $t("item.sideEffectOk")}</span>
        </div>
      {/each}
      {#if group.finished}<div class="pt-1"><Markdown content={String(group.finished.payload["summary"] ?? "").slice(0, 4000)} /></div>{/if}
    </div>
  {/if}
</Card>
