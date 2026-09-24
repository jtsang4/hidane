<script lang="ts">
  import { AlarmClock, CircleHelp, CornerDownRight, Webhook } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import type { BoardCard, EscalationStep } from "../lib/api.js";
  import { turnRouting, type Turn } from "../lib/conversation.js";
  import { payloadText } from "../lib/grouping.js";
  import { cn } from "../lib/utils.js";
  import AttributionChip from "./AttributionChip.svelte";
  import ChatBubble from "./ChatBubble.svelte";
  import TaskCard from "./TaskCard.svelte";
  import Time from "./Time.svelte";
  import Badge from "./ui/Badge.svelte";
  import Button from "./ui/Button.svelte";

  let {
    turn,
    cards,
    choices,
    titleOf,
    focused,
    highlighted = false,
    onroute,
    onfocus,
    onanswer,
    onanswerEscalation,
    onstop,
  }: {
    turn: Turn;
    cards: ReadonlyMap<string, BoardCard>;
    choices: { id: string; title: string }[];
    titleOf: (id: string) => string;
    focused: string | null;
    highlighted?: boolean;
    onroute: (messageId: string, workItemId: string) => void;
    onfocus: (id: string) => void;
    onanswer: (card: BoardCard) => void;
    onanswerEscalation: (eventId: string, workItemId: string) => void;
    onstop: (id: string) => void;
  } = $props();

  let createdCard = $derived(turn.createdItem ? cards.get(turn.createdItem) : undefined);
  let openEscalation = $derived(
    new Set([...cards.values()].map((c) => c.escalation?.id).filter((id): id is string => Boolean(id))),
  );
</script>

<section id={`turn-${turn.root}`} data-root={turn.root} class={cn("space-y-2 rounded-lg transition-colors", highlighted && "bg-primary/5 ring-1 ring-primary/30")}>
  {#if turn.message}
    <ChatBubble event={turn.message} />
    <AttributionChip {turn} {choices} {titleOf} {onroute} {onfocus} />
    {#if turnRouting(turn)}
      <div class="flex justify-end" role="status" aria-live="polite">
        <span class="flex items-center gap-1.5 text-xs text-muted">
          <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-primary" aria-hidden="true"></span>{$t("conversation.routing")}
        </span>
      </div>
    {/if}
  {:else if turn.origin}
    <p class="flex items-center justify-center gap-1.5 text-xs text-muted">
      {#if turn.origin.kind === "scheduled"}<AlarmClock size={12} aria-hidden="true" />{$t("conversation.scheduled", { name: turn.origin.text })}{:else if turn.origin.kind === "external"}<Webhook size={12} aria-hidden="true" /><span class="max-w-[70vw] truncate">{$t("conversation.external")} · {turn.origin.text}</span>{:else}{$t("conversation.earlier")}{/if}
    </p>
  {/if}

  {#if createdCard}
    <div class="flex justify-start">
      <TaskCard card={createdCard} {cards} compact focused={focused === createdCard.item.id} {onfocus} {onanswer} {onstop} />
    </div>
  {/if}

  {#each turn.answers as answer (answer.id)}
    {#if answer.kind === "execution.steered"}
      <p class="flex items-center justify-end gap-1 text-xs text-muted">
        <CornerDownRight size={12} aria-hidden="true" />{$t(answer.payload["queued"] === true ? "conversation.steeredQueued" : "conversation.steered")}
      </p>
    {:else if answer.kind === "escalation" && (answer.payload["question"] !== undefined)}
      {@const workItemId = answer.workItemId}
      {@const path = (answer.payload["path"] as EscalationStep[] | undefined) ?? []}
      <div class="flex justify-start">
        <div class={cn("max-w-[85%] rounded-lg border p-3 text-sm", openEscalation.has(answer.id) ? "border-danger/50 bg-danger/5" : "border-border bg-surface")}>
          <p class="flex items-center gap-1.5 text-xs font-medium text-danger">
            <CircleHelp size={14} aria-hidden="true" />
            {#if workItemId}{titleOf(workItemId)} · {/if}{$t(`task.reason.${answer.payload["reason"] === "budget" || answer.payload["reason"] === "deadline" ? answer.payload["reason"] : "question"}`)}
          </p>
          <p class="mt-1 whitespace-pre-wrap">{String(answer.payload["question"] ?? "")}</p>
          {#if path.some((step) => step.tried)}
            <ul class="mt-1 list-disc pl-4 text-xs text-muted">
              {#each path.filter((step) => step.tried) as step (step.workItemId)}<li><span class="text-foreground/80">{step.title}</span> — {step.tried}</li>{/each}
            </ul>
          {/if}
          <div class="mt-2 flex items-center gap-2 text-[10px] text-muted">
            {#if workItemId && openEscalation.has(answer.id)}<Button size="sm" onclick={() => onanswerEscalation(answer.id, workItemId)}>{$t("task.answer")}</Button>{/if}
            <Time iso={answer.ts} />
          </div>
        </div>
      </div>
    {:else if answer.kind === "escalation"}
      <div class="flex justify-center"><Badge tone="muted">{payloadText(answer)}</Badge></div>
    {:else}
      <div class="space-y-0.5">
        {#if answer.kind === "agent.reply" && answer.workItemId}
          <button class="ml-1 text-[10px] text-muted hover:text-foreground" onclick={() => answer.workItemId && onfocus(answer.workItemId)}>{titleOf(answer.workItemId)}</button>
        {/if}
        <ChatBubble event={answer} />
      </div>
    {/if}
  {/each}
</section>
