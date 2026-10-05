<script lang="ts">
  import AlarmClock from "@lucide/svelte/icons/alarm-clock";
  import CornerDownRight from "@lucide/svelte/icons/corner-down-right";
  import FolderGit2 from "@lucide/svelte/icons/folder-git-2";
  import Webhook from "@lucide/svelte/icons/webhook";
  import { t } from "../i18n/index.js";
  import type { BoardCard, EscalationStep, HidaneEvent } from "../lib/api.js";
  import { atPointer, keepsSystemMenu, type MenuPlacement } from "../lib/contextMenu.svelte.js";
  import { escalationText, reasonKey } from "../lib/escalation.js";
  import { isReport, steeredKey, turnRouting, type Turn } from "../lib/conversation.js";
  import { payloadText } from "../lib/grouping.js";
  import { cn } from "../lib/utils.js";
  import AttributionChip from "./AttributionChip.svelte";
  import ChatBubble from "./ChatBubble.svelte";
  import EscalationOptions from "./EscalationOptions.svelte";
  import EscalationQuestion from "./EscalationQuestion.svelte";
  import MoreButton from "./MoreButton.svelte";
  import ReportRow from "./ReportRow.svelte";
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
    routingKnown = true,
    onroute,
    onfocus,
    onanswer,
    onanswerEscalation,
    onchoose,
    onstop,
    onmessagemenu,
    ontaskmenu,
  }: {
    turn: Turn;
    cards: ReadonlyMap<string, BoardCard>;
    choices: { id: string; title: string }[];
    titleOf: (id: string) => string;
    focused: string | null;
    highlighted?: boolean;
    /**
     * Whether everything after this turn is loaded. In a window of history the
     * answers may simply lie past its end, so "still routing" cannot be told.
     */
    routingKnown?: boolean;
    onroute: (messageId: string, workItemId: string) => void;
    onfocus: (id: string) => void;
    onanswer: (card: BoardCard) => void;
    onanswerEscalation: (eventId: string, workItemId: string) => void;
    /** Answers a question with one of the answers it offers. */
    onchoose: (eventId: string, workItemId: string, option: string) => void;
    onstop: (id: string) => void;
    /** The message menu (copy, hide, link) — from a right click or the "⋯" button. */
    onmessagemenu: (event: HidaneEvent, placement: MenuPlacement) => void;
    ontaskmenu: (card: BoardCard, placement: MenuPlacement) => void;
  } = $props();

  /** A right click on a message opens its menu, unless text is selected — then the system's Copy is wanted. */
  function contextMenu(event: MouseEvent, message: HidaneEvent): void {
    if (keepsSystemMenu(event.currentTarget as Element)) return;
    event.preventDefault();
    onmessagemenu(message, atPointer(event));
  }

  let createdCard = $derived(turn.createdItem ? cards.get(turn.createdItem) : undefined);
  let openEscalation = $derived(
    new Set([...cards.values()].map((c) => c.escalation?.id).filter((id): id is string => Boolean(id))),
  );
</script>

<section id={`turn-${turn.root}`} data-root={turn.root} class={cn("space-y-2 rounded-lg transition-colors", highlighted && "bg-primary/5 ring-1 ring-primary/30")}>
  {#if turn.message}
    {@const message = turn.message}
    <div class="group/said relative" role="presentation" oncontextmenu={(event) => contextMenu(event, message)}>
      <ChatBubble event={message} hidden={turn.redacted} anchored />
      <!-- In the free space beside the bubble; revealed on hover or keyboard focus, always shown without hover. -->
      <MoreButton
        class="absolute bottom-0 left-0 opacity-0 transition-opacity group-hover/said:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
        label={$t("menu.messageMore")}
        onopen={(placement) => onmessagemenu(message, placement)}
      />
    </div>
    <AttributionChip {turn} {choices} {titleOf} {onroute} {onfocus} />
    {#if routingKnown && turnRouting(turn)}
      <div class="flex justify-end" role="status" aria-live="polite">
        <span class="flex items-center gap-1.5 text-xs text-muted">
          <span class="size-1.5 animate-ember rounded-full bg-primary" aria-hidden="true"></span>{$t("conversation.routing")}
        </span>
      </div>
    {/if}
  {:else if turn.origin}
    <p class="flex items-center justify-center gap-1.5 text-xs text-muted">
      {#if turn.origin.kind === "scheduled"}<AlarmClock size={12} aria-hidden="true" />{$t("conversation.scheduled", { name: turn.origin.text })}{:else if turn.origin.kind === "external"}<Webhook size={12} aria-hidden="true" /><span class="max-w-[70vw] truncate">{$t("conversation.external")} · {turn.origin.text}</span>{:else if turn.origin.kind === "repo"}<FolderGit2 size={12} aria-hidden="true" /><span class="max-w-[70vw] truncate">{$t("conversation.repoCheck")} · {turn.origin.text}</span>{:else}{$t("conversation.earlier")}{/if}
    </p>
  {/if}

  {#if createdCard}
    <div class="flex justify-start">
      <TaskCard
        card={createdCard}
        {cards}
        hideQuestion={turn.answers.some((answer) => answer.id === createdCard.escalation?.id)}
        focused={focused === createdCard.item.id}
        {onfocus}
        {onanswer}
        {onstop}
        onmenu={ontaskmenu}
      />
    </div>
  {/if}

  {#each turn.answers as answer (answer.id)}
    {#if answer.kind === "execution.steered"}
      <p class="flex items-center justify-end gap-1 text-xs text-muted">
        <CornerDownRight size={12} aria-hidden="true" />{$t(steeredKey(answer))}
      </p>
    {:else if answer.kind === "escalation" && (answer.payload["question"] !== undefined)}
      {@const workItemId = answer.workItemId}
      {@const reason = $t(reasonKey(answer.payload["reason"]))}
      <div class="flex justify-start">
        <div class={cn("max-w-[85%] rounded-lg border px-3 py-2.5 text-sm", openEscalation.has(answer.id) ? "border-danger/40 bg-danger/5" : "border-border bg-surface")}>
          <EscalationQuestion
            heading={workItemId ? `${titleOf(workItemId)} · ${reason}` : reason}
            text={escalationText(answer.payload)}
            path={(answer.payload["path"] as EscalationStep[] | undefined) ?? []}
            listed
          />
          {#if workItemId && openEscalation.has(answer.id)}
            {@const options = (answer.payload["options"] as unknown[] | undefined)?.filter((o): o is string => typeof o === "string") ?? []}
            {#if options.length > 0}
              <div class="mt-2"><EscalationOptions {options} onchoose={(option) => onchoose(answer.id, workItemId, option)} /></div>
            {/if}
          {/if}
          <div class="mt-2 flex items-center gap-2 text-2xs text-muted">
            {#if workItemId && openEscalation.has(answer.id)}<Button onclick={() => onanswerEscalation(answer.id, workItemId)}>{$t("task.answer")}</Button>{/if}
            <Time iso={answer.ts} />
          </div>
        </div>
      </div>
    {:else if answer.kind === "escalation"}
      <div class="flex justify-center"><Badge tone="muted">{payloadText(answer)}</Badge></div>
    {:else if isReport(answer) && answer.workItemId}
      <ReportRow event={answer} title={titleOf(answer.workItemId)} cardState={cards.get(answer.workItemId)?.state ?? null} {onfocus} onmenu={onmessagemenu} />
    {:else}
      <div class="group/answer relative space-y-0.5" role="presentation" oncontextmenu={(event) => { if (answer.kind === "agent.reply") contextMenu(event, answer); }}>
        {#if answer.kind === "agent.reply" && answer.workItemId}
          <button class="text-2xs text-muted hover:text-foreground" onclick={() => answer.workItemId && onfocus(answer.workItemId)}>{titleOf(answer.workItemId)}</button>
        {/if}
        <ChatBubble event={answer} anchored />
        {#if answer.kind === "agent.reply"}
          <MoreButton
            class="absolute right-0 bottom-0 opacity-0 transition-opacity group-hover/answer:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
            label={$t("menu.messageMore")}
            onopen={(placement) => onmessagemenu(answer, placement)}
          />
        {/if}
      </div>
    {/if}
  {/each}
</section>
