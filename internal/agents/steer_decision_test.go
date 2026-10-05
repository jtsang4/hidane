package agents_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// Words for a running worker go straight to it by rule: the Manager's model
// is not asked about them, so no decision is recorded until the run reports.
func TestSteeringRecordsNoManagerDecision(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude, "FAKEAGENT_WORKER_DELAY_MS=1500")
	item := m(w.k.CreateWorkItem(ctx, "long job", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "start", Source: "connector:web", Target: item.ID}))
	if err := w.rt.Drain(20); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, w, item.ID)
	said := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "also add a footer", Source: "connector:web", Target: item.ID}))
	w.settle()
	var copied kernel.Event
	for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "user.message", ThreadID: item.ThreadID})) {
		if e.Payload.Str("of") == said.ID {
			copied = e
		}
	}
	if copied.ID == "" {
		t.Fatalf("the Manager's copy: %v", w.kinds())
	}
	decisions := w.events("manager.decision")
	for _, d := range decisions {
		of, _ := d.Payload["of"].([]any)
		for _, id := range of {
			if id == copied.ID {
				t.Fatalf("the steered words went to the model: %+v", d.Payload)
			}
		}
	}
	steered := w.events("execution.steered")
	finished := w.events("execution.finished")
	if len(steered) != 1 || len(finished) != 1 {
		t.Fatalf("one run, steered once: %v", w.kinds())
	}
	for _, d := range decisions {
		if d.Seq > copied.Seq && d.Seq < finished[0].Seq {
			t.Fatalf("a decision while the words were steered: %+v", d.Payload)
		}
	}
	if len(decisions) != 2 {
		t.Fatalf("one decision to start, one on the outcome: %d", len(decisions))
	}
}
