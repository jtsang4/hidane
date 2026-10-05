package agents_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A fanned-out task's children report to it, not to the person, and its
// summary answers the message that started it; a child's long answer reaches
// the parent clipped, with a pointer to the rest.
func TestChildrenReportToTheParentNotThePerson(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	parent := m(w.k.CreateWorkItem(ctx, "compare", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "SPLIT_CHILDREN", Source: "connector:web", Target: parent.ID}))
	w.settle()
	children := m(w.k.ListChildren(ctx, parent.ID))
	for _, r := range w.events("agent.reply") {
		if r.WorkItemID != parent.ID && !r.Payload.Bool("child") {
			t.Fatalf("a child's answer is marked as one: %+v", r.Payload)
		}
	}
	if len(children) != 2 || w.lastReply(msg.ID) != "子任务都完成了。" {
		t.Fatalf("the parent sums up under the first message: %d children, %q", len(children), w.lastReply(msg.ID))
	}

	long := m(w.k.CreateWorkItem(ctx, "long", "test", kernel.CreateWorkItemOpts{}))
	child := m(w.k.CreateWorkItem(ctx, "long child", "test", kernel.CreateWorkItemOpts{ParentID: long.ID}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "LONG_REPLY please", Source: "connector:web", Target: child.ID}))
	w.settle()
	m(w.s.ChangeStatus(ctx, child.ID, kernel.StatusDone, "test", nil))
	w.settle()
	settled := m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "children.settled", WorkItemID: long.ID}))
	if len(settled) != 1 {
		t.Fatalf("settled: %+v", settled)
	}
	result, _ := settled[0].Payload["children"].([]any)[0].(map[string]any)["result"].(string)
	if !strings.HasPrefix(result, "开头") || !strings.Contains(result, "the full answer is in work item "+child.ID) || !strings.Contains(result, child.Workspace) {
		t.Fatalf("clipped with a pointer to the rest: …%s", result[len(result)-200:])
	}
}

type promptLog struct {
	agentcli.Starter
	mu      sync.Mutex
	prompts []string
}

func (p *promptLog) Start(ctx context.Context, agent string, req agentcli.Request) (agentcli.Run, error) {
	p.mu.Lock()
	p.prompts = append(p.prompts, req.Prompt)
	p.mu.Unlock()
	return p.Starter.Start(ctx, agent, req)
}

// A child the model described badly is not dropped in silence: the error
// stays in front of the Manager on its later turns.
func TestASkippedChildStaysInTheManagersHistory(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	log := &promptLog{Starter: w.s.Agents}
	w.s.Agents = log
	item := m(w.k.CreateWorkItem(ctx, "parent", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "BAD_CHILDREN", Source: "connector:web", Target: item.ID}))
	w.settle()
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "再看看", Source: "connector:web", Target: item.ID}))
	w.settle()
	log.mu.Lock()
	defer log.mu.Unlock()
	last := ""
	for _, p := range log.prompts {
		if strings.Contains(p, "再看看") {
			last = p
		}
	}
	if !strings.Contains(last, `[agent.error] child #1 of 2 ("调研 A 的价格") was not created`) {
		t.Fatalf("the later turn sees the error:\n%s", last)
	}
}
