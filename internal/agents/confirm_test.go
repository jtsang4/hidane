package agents_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// repliesTo are the answers under one message.
func (w *world) repliesTo(msg kernel.Event) []string {
	var out []string
	for _, r := range w.events("agent.reply") {
		if r.Payload.Str("root") == msg.ID {
			out = append(out, r.Payload.Str("text"))
		}
	}
	return out
}

// Closing everything on its own gets one answer: what was changed, in place
// of the model's reply, written before anything happened.
func TestCloseEverythingIsConfirmedOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	m(w.k.CreateWorkItem(ctx, "a", "test", kernel.CreateWorkItemOpts{}))
	m(w.k.CreateWorkItem(ctx, "b", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "关闭所有任务", Source: "connector:web"}))
	w.settle()
	if replies := w.repliesTo(msg); len(replies) != 1 || !strings.HasPrefix(replies[0], "已将 2 个工作项的状态设为 closed") {
		t.Fatalf("one confirmation of what was closed: %q", replies)
	}
	if open := m(w.k.ListWorkItems(ctx, kernel.StatusOpen)); len(open) != 0 {
		t.Fatalf("all closed: %+v", open)
	}
}

// Stopping a running task on its own gets one answer too.
func TestStopIsConfirmedOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude, "FAKEAGENT_WORKER_DELAY_MS=20000")
	item := m(w.k.CreateWorkItem(ctx, "long", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "work", Source: "connector:web", Target: item.ID}))
	if err := w.rt.Drain(20); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, w, item.ID)
	start := time.Now()
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "停止正在跑的", Source: "connector:web"}))
	w.settle()
	if time.Since(start) > 15*time.Second {
		t.Fatal("the stop waited out the run")
	}
	if replies := w.repliesTo(msg); len(replies) != 1 || !strings.HasPrefix(replies[0], "已请求中止 1 个正在运行的执行") {
		t.Fatalf("one confirmation of what was stopped: %q", replies)
	}
	if finished := w.events("execution.finished"); len(finished) != 1 || !finished[0].Payload.Bool("cancelled") {
		t.Fatalf("the run was cancelled: %+v", finished)
	}
}
