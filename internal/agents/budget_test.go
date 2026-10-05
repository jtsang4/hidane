package agents_test

import (
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A chain stopped by the hop budget is a question in the conversation, and the
// person's answer starts a new chain: its hops count from zero, so the work
// really goes on.
func TestTheHopBudgetAsksOnTheMainThreadAndAnAnswerStartsAfresh(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	// Message (0) → the Manager's copy (1) → a worker (2) → its outcome (3):
	// the second worker the fake asks for would be hop 4.
	w.k.Cfg.MaxHops = 2
	item := m(w.k.CreateWorkItem(ctx, "two rounds", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "do it AGAIN", Source: "connector:web", Target: item.ID}))
	w.settle()
	var budget []kernel.Event
	for _, e := range w.events("escalation") {
		if e.Payload.Str("reason") == "budget" {
			budget = append(budget, e)
		}
	}
	if len(budget) != 1 || m(w.k.CountExecutions(ctx, item.ID)) != 1 {
		t.Fatalf("the second dispatch is refused with a question: %+v (kinds %v)", budget, w.kinds())
	}
	asked := budget[0]
	if asked.Source != "kernel:runtime" || asked.ThreadID != "main" || asked.WorkItemID != item.ID || asked.Payload.Str("root") != msg.ID {
		t.Fatalf("the question is on the main thread, under the message: %+v", asked)
	}
	shown := false
	for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{Conversation: true})) {
		shown = shown || e.ID == asked.ID
	}
	if !shown {
		t.Fatal("the conversation shows the question")
	}
	for _, r := range w.events("agent.reply") {
		if strings.Contains(r.Payload.Str("text"), "已派出第二个 worker") {
			t.Fatal("a reply announcing the refused dispatch must not reach the person")
		}
	}

	answer := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "继续", Source: "connector:web", ReplyTo: asked.ID}))
	w.settle()
	if answer.Hop != 0 || answer.CausedBy != "" {
		t.Fatalf("the person's answer starts a chain of its own: hop %d, caused by %q", answer.Hop, answer.CausedBy)
	}
	var copied kernel.Event
	for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "user.message", ThreadID: item.ThreadID})) {
		if e.Payload.Str("of") == answer.ID {
			copied = e
		}
	}
	if copied.Hop != 1 || copied.Payload.Str("answers") != asked.ID {
		t.Fatalf("the Manager's copy is one hop on, as an answer: %+v", copied)
	}
	if n := m(w.k.CountExecutions(ctx, item.ID)); n != 2 {
		t.Fatalf("after the answer a worker runs again: %d executions (kinds %v)", n, w.kinds())
	}
}
