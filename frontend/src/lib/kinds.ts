// The kernel's kind lists (internal/kernel/events.go), for filtering live
// events; frontend/kinds_test.go holds them equal.

/** Kinds that answer a message and name it in `payload.root` (`AnswerKinds`). */
export const ANSWER_KINDS: ReadonlySet<string> = new Set(["agent.reply", "escalation", "attribution.ambiguous", "agent.error", "execution.steered"]);

/** Rendered from the main thread (`ConversationMainKinds`). */
const MAIN_KINDS = new Set(["user.message", "message.attributed", "message.redacted", ...ANSWER_KINDS]);

/** Rendered whichever thread they were written on (`ConversationAnyKinds`). */
const ANY_THREAD_KINDS = new Set(["agent.reply", "execution.steered"]);

/** Does the conversation show this event? The kernel's `Conversation` filter. */
export function inConversation(event: { kind: string; threadId: string | null }): boolean {
  return (event.threadId === "main" && MAIN_KINDS.has(event.kind)) || ANY_THREAD_KINDS.has(event.kind);
}
