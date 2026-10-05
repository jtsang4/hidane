package agents_test

import (
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
)

func ofItem(events []kernel.Event, itemID string) []kernel.Event {
	var out []kernel.Event
	for _, e := range events {
		if e.WorkItemID == itemID {
			out = append(out, e)
		}
	}
	return out
}

// A message the person moved before its first Manager read it is no longer
// that Manager's to act on: acceptance found both Managers working on it.
func TestAMessageMovedBeforeItWasReadIsSkipped(t *testing.T) {
	t.Parallel()
	w, rec := markerWorld(t)
	from := m(w.k.CreateWorkItem(ctx, "login page", "test", kernel.CreateWorkItemOpts{}))
	to := m(w.k.CreateWorkItem(ctx, "register page", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "按钮颜色再深一点", Source: "connector:web", Target: from.ID}))
	m(w.s.Reattribute(ctx, msg.ID, to.ID, "connector:web"))
	w.settle()

	if d := ofItem(w.events("manager.decision"), from.ID); len(d) != 0 {
		t.Fatalf("the Manager the message was moved away from decided on it: %+v", d)
	}
	if s := ofItem(w.events("execution.started"), from.ID); len(s) != 0 {
		t.Fatalf("the Manager the message was moved away from started a worker: %+v", s)
	}
	rec.mu.Lock()
	for _, req := range rec.reqs {
		if req.SystemPrompt == agents.ManagerCharter && strings.Contains(req.Prompt, "Work item: "+from.ID) {
			t.Errorf("a turn left with nothing to do made a model call:\n%s", req.Prompt)
		}
	}
	rec.mu.Unlock()
	if d := ofItem(w.events("manager.decision"), to.ID); len(d) == 0 {
		t.Fatalf("the Manager the message was moved to never read it: %v", w.kinds())
	}
}

// A message sent to several work items at once is attributed to each in turn:
// none of those copies was moved anywhere.
func TestAMessageSentToSeveralItemsIsReadByEach(t *testing.T) {
	t.Parallel()
	w, _ := markerWorld(t)
	a := m(w.k.CreateWorkItem(ctx, "notes a", "test", kernel.CreateWorkItemOpts{}))
	b := m(w.k.CreateWorkItem(ctx, "notes b", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "各自写一份", Source: "connector:web", Targets: []string{a.ID, b.ID}}))
	w.settle()
	for _, item := range []kernel.WorkItem{a, b} {
		if d := ofItem(w.events("manager.decision"), item.ID); len(d) == 0 {
			t.Fatalf("%s never read the message: %v", item.ID, w.kinds())
		}
	}
}

// A Manager that already acted on a message is told, from then on, that the
// person moved it elsewhere: the move is recorded on the item it went to.
func TestAManagerIsToldAMessageItActedOnWasMoved(t *testing.T) {
	t.Parallel()
	w, rec := markerWorld(t)
	from := m(w.k.CreateWorkItem(ctx, "login page", "test", kernel.CreateWorkItemOpts{}))
	to := m(w.k.CreateWorkItem(ctx, "register page", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "按钮颜色再深一点", Source: "connector:web", Target: from.ID}))
	w.settle()
	if s := ofItem(w.events("execution.started"), from.ID); len(s) != 1 {
		t.Fatalf("the first Manager acts on the message: %v", w.kinds())
	}
	m(w.s.Reattribute(ctx, msg.ID, to.ID, "connector:web"))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "标题改成 Login", Source: "connector:web", Target: from.ID}))
	w.settle()

	want := "[moved] the person moved message " + msg.ID
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var last string
	for _, req := range rec.reqs {
		if req.SystemPrompt == agents.ManagerCharter && strings.Contains(req.Prompt, "Work item: "+from.ID) {
			last = req.Prompt
		}
	}
	history, _, _ := strings.Cut(last, "Messages this turn:")
	line := ""
	for _, l := range strings.Split(history, "\n") {
		if strings.HasPrefix(l, want) {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, to.ID) || !strings.Contains(line, "按钮颜色再深一点") {
		t.Fatalf("the old Manager's next turn must be told of the move (%q):\n%s", want, last)
	}
}
