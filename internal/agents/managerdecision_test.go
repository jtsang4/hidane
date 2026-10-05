package agents_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A Manager answer that was not the effect list is kept on its decision, as
// the Primary's is: `effects: []` alone left nothing to diagnose it by.
func TestManagerDecisionKeepsOutputThatIsNotTheEffectList(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "junk", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "JUNK_ONCE 写一个文件", Source: "connector:web", Target: item.ID}))
	w.settle()
	decisions := w.events("manager.decision")
	if len(decisions) < 2 {
		t.Fatalf("decisions: %+v", decisions)
	}
	first, _ := decisions[0].Payload["effects"].([]any)
	if len(first) != 1 {
		t.Fatalf("the junk answer must be on record: %+v", decisions[0].Payload)
	}
	if e, _ := first[0].(map[string]any); e["type"] != "reply" || e["raw"] != "response." {
		t.Fatalf("the junk answer must be on record as the Primary's is: %+v", first[0])
	}
	nudged, _ := decisions[1].Payload["effects"].([]any)
	for _, raw := range nudged {
		if e, _ := raw.(map[string]any); e["raw"] != nil {
			t.Fatalf("an effect list is recorded as parsed: %+v", nudged)
		}
	}
	if len(nudged) == 0 {
		t.Fatalf("the nudged turn's effects: %+v", decisions[1].Payload)
	}
}
