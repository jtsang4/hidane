package agents_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
	"github.com/jtsang4/hidane/internal/settings"
)

// A Manager's question reaches the person by rule, with what each level
// tried; the answer goes back to that Manager as an answer, and the task
// stops waiting.
func TestAQuestionGoesUpAndItsAnswerComesBack(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "flights", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "订机票 ASK_OPTIONS", Source: "connector:web", Target: item.ID}))
	w.settle()
	if raised := w.events("escalation.raised"); len(raised) != 1 || raised[0].Mailbox != kernel.Primary {
		t.Fatalf("raised to the Primary: %+v", raised)
	}
	esc := w.events("escalation")
	if len(esc) != 1 || esc[0].ThreadID != "main" || esc[0].WorkItemID != item.ID || esc[0].Payload.Str("question") != "出发时间选哪个？" {
		t.Fatalf("the question on the main thread: %+v", esc)
	}
	path, _ := esc[0].Payload["path"].([]any)
	if len(path) != 1 || path[0].(map[string]any)["tried"] != "查过两天的航班" || path[0].(map[string]any)["workItemId"] != item.ID {
		t.Fatalf("each level says what it tried: %+v", esc[0].Payload)
	}
	if n := len(w.events("route.decision")); n != 0 {
		t.Fatalf("surfaced by rule, without a model call: %d", n)
	}
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "周六早上", Source: "connector:web", ReplyTo: esc[0].ID}))
	w.settle()
	var answered bool
	for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "user.message", WorkItemID: item.ID})) {
		answered = answered || e.ThreadID == item.ThreadID && e.Payload.Str("answers") == esc[0].ID
	}
	if !answered {
		t.Fatal("the answer reaches the Manager that asked, as an answer")
	}
	if card := m(projections.BuildBoard(ctx, w.k, nil))[0]; card.State == "waiting" {
		t.Fatalf("answered, the task no longer waits: %+v", card)
	}
}
