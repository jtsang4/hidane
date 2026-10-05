package kernel_test

import (
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
)

// The escalation counts the hops the chain took, not the one it was refused:
// with HIDANE_MAX_HOPS=3 it said "连续触发 4 次".
func TestBudgetEscalationCountsTheHopsTaken(t *testing.T) {
	k := kerneltest.New(t)
	k.Cfg.MaxHops = 3
	post := func(cause *kernel.Event) (kernel.Event, bool) {
		return m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "test", Kind: "m", Mailbox: "primary"}, CausedBy: cause}))
	}
	last, _ := post(nil)
	for range k.Cfg.MaxHops {
		next, ok := post(&last)
		if !ok {
			t.Fatalf("hop %d refused within the budget", last.Hop+1)
		}
		last = next
	}
	if _, ok := post(&last); ok {
		t.Fatal("hop budget must stop the chain")
	}
	esc := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "escalation"}))
	if len(esc) != 1 {
		t.Fatalf("escalations: %+v", esc)
	}
	if q := esc[0].Payload.Str("question"); !strings.Contains(q, "连续触发 3 次") {
		t.Fatalf("question %q must count the 3 hops the chain took", q)
	}
}
