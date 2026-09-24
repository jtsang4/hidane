<script lang="ts">
  import { CornerDownRight, Plus } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import type { Turn } from "../lib/conversation.js";
  import { cn } from "../lib/utils.js";

  let {
    turn,
    choices,
    titleOf,
    onroute,
    onfocus,
  }: {
    turn: Turn;
    /** Open work items the message could be moved to. */
    choices: { id: string; title: string }[];
    titleOf: (id: string) => string;
    onroute: (messageId: string, workItemId: string) => void;
    onfocus: (id: string) => void;
  } = $props();

  let open = $state(false);
  let current = $derived(turn.attribution?.workItemId ?? null);
  let by = $derived(String(turn.attribution?.payload["by"] ?? "model"));
  let candidates = $derived(
    ((turn.ambiguous?.payload["candidates"] as { workItemId: string; title: string }[] | undefined) ?? []).map((c) => ({
      id: c.workItemId,
      title: c.workItemId === "new" ? $t("attribution.newTask") : c.title || titleOf(c.workItemId),
    })),
  );
  let others = $derived(choices.filter((c) => c.id !== current));

  function pick(workItemId: string): void {
    open = false;
    if (turn.message) onroute(turn.message.id, workItemId);
  }
</script>

<svelte:window onkeydown={(event) => { if (event.key === "Escape") open = false; }} />

{#if turn.ambiguous && turn.message}
  <div class="flex justify-end">
    <div class="max-w-[85%] rounded-lg border border-primary/40 bg-primary/5 px-3 py-2 text-xs" role="group" aria-label={$t("attribution.pick")}>
      <p class="text-foreground/90">{String(turn.ambiguous.payload["question"] ?? "")}</p>
      <div class="mt-2 flex flex-wrap justify-end gap-1.5">
        {#each candidates as candidate (candidate.id)}
          <button class="flex max-w-full items-center gap-0.5 rounded-full border border-primary/50 px-2.5 py-1 text-left text-primary hover:bg-primary/10" onclick={() => pick(candidate.id)}>
            {#if candidate.id === "new"}<Plus size={10} class="shrink-0" />{/if}<span class="truncate">{candidate.title}</span>
          </button>
        {/each}
      </div>
    </div>
  </div>
{:else if current && turn.message}
  <div class="relative flex items-center justify-end gap-1.5 text-xs text-muted">
    <CornerDownRight size={12} aria-hidden="true" />
    <button class="max-w-[60vw] truncate text-foreground/80 underline-offset-2 hover:underline" onclick={() => onfocus(current)}>
      {turn.createdItem === current ? $t("attribution.createdAs", { title: titleOf(current) }) : $t("attribution.routedTo", { title: titleOf(current) })}
    </button>
    <span class="hidden sm:inline">· {$t(`attribution.by.${by === "explicit" || by === "focus" || by === "user" ? by : "model"}`)}</span>
    <button class="rounded px-1 text-primary hover:bg-primary/10" aria-expanded={open} aria-label={$t("attribution.changeLabel")} onclick={() => (open = !open)}>
      {$t("attribution.change")}
    </button>
    {#if open}
      <div class="absolute right-0 bottom-full z-20 mb-1 max-h-64 w-64 overflow-y-auto rounded-md border border-border bg-surface p-1 shadow-lg" role="menu">
        {#each others as choice (choice.id)}
          <button class="block w-full truncate rounded px-2 py-1.5 text-left text-foreground hover:bg-surface-2" role="menuitem" onclick={() => pick(choice.id)}>{choice.title}</button>
        {/each}
        <button class={cn("flex w-full items-center gap-1 rounded px-2 py-1.5 text-left text-primary hover:bg-surface-2", others.length > 0 && "border-t border-border")} role="menuitem" onclick={() => pick("new")}>
          <Plus size={12} />{$t("attribution.newTask")}
        </button>
      </div>
    {/if}
  </div>
{/if}
