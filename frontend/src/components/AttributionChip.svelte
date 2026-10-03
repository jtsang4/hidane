<script lang="ts">
  import { DropdownMenu } from "bits-ui";
  import { CornerDownRight, Plus } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import type { Turn } from "../lib/conversation.js";
  import { popover, popoverItem } from "../lib/styles.js";
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

{#if turn.ambiguous && turn.message}
  <div class="flex justify-end">
    <div class="max-w-[85%] rounded-lg border border-border bg-surface px-3 py-2 text-xs" role="group" aria-label={$t("attribution.pick")}>
      <p class="text-foreground/90">{String(turn.ambiguous.payload["question"] ?? "")}</p>
      <div class="mt-2 flex flex-wrap justify-end gap-1.5">
        {#each candidates as candidate (candidate.id)}
          <button class="flex h-6 max-w-full items-center gap-1 rounded-md bg-primary/12 px-2 text-left text-primary hover:bg-primary/20 coarse:min-h-9" onclick={() => pick(candidate.id)}>
            {#if candidate.id === "new"}<Plus size={10} class="shrink-0" />{/if}<span class="truncate">{candidate.title}</span>
          </button>
        {/each}
      </div>
    </div>
  </div>
{:else if current && turn.message}
  <div class="flex items-center justify-end gap-1.5 text-xs text-muted">
    <CornerDownRight size={12} aria-hidden="true" />
    <button class="max-w-[60vw] truncate text-foreground/80 underline-offset-2 hover:underline" onclick={() => onfocus(current)}>
      {turn.createdItem === current ? $t("attribution.createdAs", { title: titleOf(current) }) : $t("attribution.routedTo", { title: titleOf(current) })}
    </button>
    <span class="hidden sm:inline">· {$t(`attribution.by.${by === "explicit" || by === "focus" || by === "user" ? by : "model"}`)}</span>
    <DropdownMenu.Root bind:open>
      <DropdownMenu.Trigger class="-mr-1 rounded px-1 text-primary hover:bg-primary/10 data-[state=open]:bg-primary/10" aria-label={$t("attribution.changeLabel")}>
        {$t("attribution.change")}
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side="top" align="end" sideOffset={4} collisionPadding={8} loop class={cn(popover, "max-h-64 w-64 overflow-y-auto")}>
          {#each others as choice (choice.id)}
            <DropdownMenu.Item class={cn(popoverItem, "block truncate")} onSelect={() => pick(choice.id)}>{choice.title}</DropdownMenu.Item>
          {/each}
          <DropdownMenu.Item class={cn(popoverItem, "text-primary", others.length > 0 && "mt-1 rounded-t-none border-t border-border pt-1.5")} onSelect={() => pick("new")}>
            <Plus size={12} />{$t("attribution.newTask")}
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  </div>
{/if}
