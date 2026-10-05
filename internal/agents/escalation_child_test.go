package agents_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A question goes up one level at a time: a child's reaches its parent's
// Manager, which may answer it, not the Primary and the person directly.
func TestAChildsQuestionGoesToItsParent(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	parent := m(w.k.CreateWorkItem(ctx, "trip", "test", kernel.CreateWorkItemOpts{}))
	child := m(w.k.CreateWorkItem(ctx, "flights", "test", kernel.CreateWorkItemOpts{ParentID: parent.ID}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "订机票 ASK_OPTIONS", Source: "connector:web", Target: child.ID}))
	w.settle()
	raised := w.events("escalation.raised")
	if len(raised) != 1 || raised[0].WorkItemID != child.ID || raised[0].Mailbox != kernel.ManagerAddress(parent.ID) {
		t.Fatalf("the child's question goes to its parent's mailbox: %+v", raised)
	}
	if esc := w.events("escalation"); len(esc) != 0 {
		t.Fatalf("the parent has it first, not the person: %+v", esc)
	}
	read := false
	for _, d := range m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "manager.decision", WorkItemID: parent.ID})) {
		of, _ := d.Payload["of"].([]any)
		for _, id := range of {
			read = read || id == raised[0].ID
		}
	}
	if !read {
		t.Fatalf("the parent's Manager decides on the question: %v", w.kinds())
	}
}
