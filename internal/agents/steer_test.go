package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// Words for a task whose worker has not started are neither lost nor planned
// twice: what piles up while the Manager plans is one decision, and what
// arrives while the worker waits in the queue joins its instructions.
func TestWordsBeforeTheWorkerStartsJoinItsInstructions(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude, "FAKEAGENT_WORKER_DELAY_MS=3000")
	w.k.Cfg.MaxWorkers = 1
	busy := m(w.k.CreateWorkItem(ctx, "busy", "test", kernel.CreateWorkItemOpts{}))
	item := m(w.k.CreateWorkItem(ctx, "notes", "test", kernel.CreateWorkItemOpts{}))
	say := func(target, text string) {
		m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: text, Source: "connector:web", Target: target}))
	}
	drain := func() {
		if err := w.rt.Drain(20); err != nil {
			t.Fatal(err)
		}
	}
	say(busy.ID, "写 busy")
	drain()
	say(item.ID, "写 notes")
	say(item.ID, "标题用中文")
	drain()
	decisions := m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "manager.decision", WorkItemID: item.ID}))
	if len(decisions) != 1 {
		t.Fatalf("one decision: %+v", decisions)
	}
	if of, _ := decisions[0].Payload["of"].([]any); len(of) != 2 {
		t.Fatalf("over both messages: %+v", decisions[0].Payload)
	}
	if ex, ok, _ := w.k.ActiveExecutionFor(ctx, item.ID); !ok || ex.Status != kernel.ExecQueued {
		t.Fatalf("the worker waits for the only slot: %+v", ex)
	}
	say(item.ID, "RUN: echo amended > amended.txt")
	drain()
	steered := m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "execution.steered", WorkItemID: item.ID}))
	if len(steered) != 1 || !steered[0].Payload.Bool("queued") {
		t.Fatalf("steered into the queued worker: %+v", steered)
	}
	w.settle()
	if _, err := os.Stat(filepath.Join(item.Workspace, "amended.txt")); err != nil {
		t.Fatal("the queued worker was given the words")
	}
	if n := len(m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "execution.started", WorkItemID: item.ID}))); n != 1 {
		t.Fatalf("one execution: %d", n)
	}
}
