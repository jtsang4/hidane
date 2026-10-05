<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import Check from "@lucide/svelte/icons/check";
  import ChevronRight from "@lucide/svelte/icons/chevron-right";
  import CircleHelp from "@lucide/svelte/icons/circle-question-mark";
  import GitBranch from "@lucide/svelte/icons/git-branch";
  import InboxIcon from "@lucide/svelte/icons/inbox";
  import MessageSquareText from "@lucide/svelte/icons/message-square-text";
  import { SvelteMap, SvelteSet } from "svelte/reactivity";
  import i18n, { t } from "../i18n/index.js";
  import { api, type BoardCard } from "../lib/api.js";
  import { attentionCards, STATE_DOT, stateTone } from "../lib/board.js";
  import { escalationText } from "../lib/escalation.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
  import { setTaskStatus } from "../lib/taskActions.js";
  import { pushToast, toastError } from "../lib/toast.js";
  import { ui } from "../lib/ui.svelte.js";
  import { cn } from "../lib/utils.js";
  import ChangesView from "../components/ChangesView.svelte";
  import EmptyState from "../components/EmptyState.svelte";
  import EscalationOptions from "../components/EscalationOptions.svelte";
  import Markdown from "../components/Markdown.svelte";
  import Page from "../components/Page.svelte";
  import Time from "../components/Time.svelte";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Textarea from "../components/ui/Textarea.svelte";

  const queryClient = useQueryClient();
  const boardQuery = createQuery(() => ({ queryKey: ["board"], queryFn: () => api.board() }));
  let queue = $derived(attentionCards(boardQuery.data?.cards ?? []));
  let questions = $derived(queue.filter((card) => card.state === "waiting"));
  let reviews = $derived(queue.filter((card) => card.state === "review"));
  /** Answers being written, per question. */
  const drafts = new SvelteMap<string, string>();
  /** Questions with an answer on its way. */
  const sending = new SvelteSet<string>();
  /** Reviews whose changes are open. */
  const showing = new SvelteSet<string>();

  async function answer(card: BoardCard, text: string): Promise<void> {
    const escalation = card.escalation;
    const body = text.trim();
    if (!escalation || !body || sending.has(escalation.id)) return;
    sending.add(escalation.id);
    try {
      await api.chat(body, [], { target: card.item.id, replyTo: escalation.id });
      drafts.delete(escalation.id);
      pushToast(i18n.t("inbox.answered"), "default");
      void queryClient.invalidateQueries({ queryKey: ["board"] });
      void queryClient.invalidateQueries({ queryKey: ["conversation"] });
    } catch (error) {
      toastError(error);
    } finally {
      sending.delete(escalation.id);
    }
  }

  /** Further words for the task go through the conversation, with the task addressed. */
  function followUp(card: BoardCard): void {
    navigate(focusHref(card.item.id));
    ui.composerFocus = true;
  }

  function toggleChanges(id: string): void {
    if (showing.has(id)) showing.delete(id);
    else showing.add(id);
  }
</script>

{#snippet heading(card: BoardCard)}
  <header class="flex items-start gap-2">
    <span aria-hidden="true" class={cn("mt-[7px] size-1.5 shrink-0 rounded-full", STATE_DOT[card.state])}></span>
    <div class="min-w-0 flex-1">
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="min-w-0 font-medium break-words">{card.item.title}</h3>
        <Badge tone={stateTone(card.state)}>{$t(`task.state.${card.state}`)}</Badge>
      </div>
      {#each card.checkouts.filter((c) => c.status === "active") as c (c.id)}
        <p class="mt-0.5 flex min-w-0 items-center gap-1 text-xs text-muted"><GitBranch size={12} class="shrink-0" aria-hidden="true" /><span class="shrink-0">{c.repo}</span><span class="truncate font-mono">{c.branch}</span></p>
      {/each}
    </div>
    <Button variant="ghost" size="sm" onclick={() => navigate(focusHref(card.item.id))}><MessageSquareText size={12} />{$t("inbox.openTask")}</Button>
  </header>
{/snippet}

<Page title={$t("nav.inbox")}>
  <p class="text-xs text-muted">{$t("inbox.subtitle")}</p>
  {#if boardQuery.data && queue.length === 0}
    <EmptyState icon={InboxIcon} text={$t("inbox.empty")} />
  {/if}

  {#if questions.length > 0}
    <section class="space-y-2" aria-labelledby="inbox-questions">
      <h2 id="inbox-questions" class="text-sm font-medium">{$t("inbox.questions")}</h2>
      {#each questions as card (card.item.id)}
        {@const escalation = card.escalation}
        {#if escalation}
          {@const draft = drafts.get(escalation.id) ?? ""}
          <article class="space-y-2 rounded-lg border border-danger/40 bg-surface p-3 text-sm" aria-label={card.item.title}>
            {@render heading(card)}
            <div class="rounded-md bg-danger/8 p-2">
              <p class="flex items-center gap-1.5 text-xs font-medium text-danger"><CircleHelp size={13} aria-hidden="true" />{$t(`task.reason.${escalation.reason === "budget" || escalation.reason === "repo_missing" ? escalation.reason : "question"}`)} · <Time iso={escalation.ts} /></p>
              <p class="mt-1 whitespace-pre-wrap select-text">{escalationText({ reason: escalation.reason, question: escalation.question })}</p>
              {#if escalation.path.some((step) => step.tried)}
                <details class="mt-1 text-xs text-muted">
                  <summary class="cursor-pointer">{$t("task.tried")}</summary>
                  <ul class="mt-1 list-disc pl-4">
                    {#each escalation.path.filter((step) => step.tried) as step (step.workItemId)}<li><span class="text-foreground/80">{step.title}</span> — {step.tried}</li>{/each}
                  </ul>
                </details>
              {/if}
            </div>
            <EscalationOptions options={escalation.options} disabled={sending.has(escalation.id)} onchoose={(option) => void answer(card, option)} />
            <div class="flex items-end gap-2">
              <Textarea
                rows={1}
                value={draft}
                aria-label={$t("inbox.answerFor", { title: card.item.title })}
                placeholder={$t("inbox.answerPlaceholder")}
                oninput={(event) => drafts.set(escalation.id, (event.currentTarget as HTMLTextAreaElement).value)}
                onkeydown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.isComposing) { event.preventDefault(); void answer(card, draft); } }}
              />
              <Button disabled={!draft.trim() || sending.has(escalation.id)} onclick={() => void answer(card, draft)}>{$t("task.answer")}</Button>
            </div>
          </article>
        {/if}
      {/each}
    </section>
  {/if}

  {#if reviews.length > 0}
    <section class="space-y-2" aria-labelledby="inbox-reviews">
      <h2 id="inbox-reviews" class="text-sm font-medium">{$t("inbox.reviews")}</h2>
      {#each reviews as card (card.item.id)}
        <article class="space-y-2 rounded-lg border border-border bg-surface p-3 text-sm" aria-label={card.item.title}>
          {@render heading(card)}
          {#if card.lastReply}
            <div class="rounded-md bg-surface-2 px-3 py-2 shadow-hairline">
              <p class="mb-1 flex items-center gap-1.5 text-2xs text-muted">{$t("inbox.latest")} · <Time iso={card.lastReply.ts} /></p>
              <div class="max-h-60 overflow-y-auto"><Markdown content={card.lastReply.text} class="select-text" /></div>
            </div>
          {/if}
          {#if card.checkouts.some((c) => c.status === "active")}
            <button class="flex items-center gap-1 text-xs text-primary hover:underline" aria-expanded={showing.has(card.item.id)} onclick={() => toggleChanges(card.item.id)}>
              <ChevronRight size={12} class={cn("transition-transform", showing.has(card.item.id) && "rotate-90")} aria-hidden="true" />{$t("review.changes")}
            </button>
            {#if showing.has(card.item.id)}<ChangesView workItemId={card.item.id} heading={false} />{/if}
          {/if}
          <div class="flex flex-wrap items-center gap-2">
            <Button variant="secondary" onclick={() => void setTaskStatus(queryClient, card.item.id, "done")}><Check size={12} />{$t("item.markDone")}</Button>
            <Button variant="secondary" onclick={() => followUp(card)}>{$t("inbox.followUp")}</Button>
          </div>
        </article>
      {/each}
    </section>
  {/if}
</Page>
