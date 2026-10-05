package agents_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agentcli/fakecli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/connectors"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// recorder keeps every request hidane makes of a CLI on its way to the fake.
type recorder struct {
	next agentcli.Starter
	mu   sync.Mutex
	reqs []agentcli.Request
}

func (r *recorder) Start(ctx context.Context, agent string, req agentcli.Request) (agentcli.Run, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.next.Start(ctx, agent, req)
}

// expect names each marker that no prompt sent under the charter holds.
func (r *recorder) expect(t *testing.T, role, charter string, markers map[string]string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, marker := range markers {
		found := false
		for _, req := range r.reqs {
			if req.SystemPrompt == charter && strings.Contains(req.Prompt, marker) {
				found = true
			}
		}
		if !found {
			t.Errorf("fakecli.%s %q is in no %s prompt hidane sent", name, marker, role)
		}
	}
}

// lines is one line format the fake reads, where it reads it (from the first
// `after` on), and the lines it must find there: a description of each, and
// whether a match is that line.
type lines struct {
	name, pattern, after string
	want                 map[string]func(match []string) bool
}

// expectLines names each wanted line that the format reads from no prompt
// sent under the charter.
func (r *recorder) expectLines(t *testing.T, role, charter string, l lines) {
	t.Helper()
	re := regexp.MustCompile(l.pattern)
	r.mu.Lock()
	defer r.mu.Unlock()
	for what, is := range l.want {
		found := false
		for _, req := range r.reqs {
			_, read, ok := strings.Cut(req.Prompt, l.after)
			if req.SystemPrompt != charter || !ok {
				continue
			}
			for _, match := range re.FindAllStringSubmatch(read, -1) {
				found = found || is(match)
			}
		}
		if !found {
			t.Errorf("fakecli.%s reads no %s from any %s prompt hidane sent", l.name, what, role)
		}
	}
}

// messageLine is a fakecli.MessageLine match for one message: its id, the
// word its parenthesis opens with, and its text ("" for any).
func messageLine(id, kind, text string) func([]string) bool {
	return func(m []string) bool {
		return m[1] == id && (kind == "" || m[2] == kind) && (text == "" || strings.TrimSpace(m[3]) == text)
	}
}

func markerWorld(t *testing.T, extraEnv ...string) (*world, *recorder) {
	w := newWorld(t, settings.Claude, extraEnv...)
	rec := &recorder{next: w.s.Agents}
	w.s.Agents = rec
	return w, rec
}

func waitRunning(t *testing.T, w *world, itemID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ex, ok, _ := w.k.ActiveExecutionFor(ctx, itemID); ok && ex.Status == kernel.ExecRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("execution never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The fake CLIs tell roles and situations apart by words of the charters and
// turn prompts (the fakecli markers), and read messages out of their lines.
// Reworded, the fake would answer as if nothing had happened and some
// unrelated test would fail; here it fails by the marker's name.
func TestFakeMarkersAreInWhatHidaneSends(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ charter, marker string }{
		"PrimaryRole":   {agents.PrimaryCharter, fakecli.PrimaryRole},
		"ManagerRole":   {agents.ManagerCharter, fakecli.ManagerRole},
		"WorkerRole":    {agents.WorkerCharter, fakecli.WorkerRole},
		"DistillerRole": {agents.DistillerCharter, fakecli.DistillerRole},
		"PingRole":      {agents.PingCharter, fakecli.PingRole},
	} {
		if !strings.Contains(c.charter, c.marker) {
			t.Errorf("fakecli.%s %q is not in its charter", name, c.marker)
		}
	}

	// A person's message, a webhook triage woke it for, and a recall's findings.
	t.Run("primary", func(t *testing.T) {
		t.Parallel()
		w, rec := markerWorld(t)
		msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好 JUNK_PRIMARY", Source: "connector:web"}))
		m(w.k.Append(ctx, kernel.EventInput{Source: "connector:webhook", Kind: "connector.webhook", Payload: kernel.Payload{"note": "deployed"}}))
		if _, _, err := connectors.TriageOnce(ctx, w.k); err != nil {
			t.Fatal(err)
		}
		woke := w.events("triage.decision")
		if len(woke) != 1 || woke[0].Mailbox != kernel.Primary {
			t.Fatalf("triage wakes the Primary: %+v", woke)
		}
		recalled, _, err := w.k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:primary", Kind: "conversation.recalled",
			Mailbox: kernel.Primary, Lane: kernel.LaneInterrupt, ThreadID: "main",
			Payload: kernel.Payload{"of": msg.ID, "root": msg.ID, "query": "deploy", "original": "earlier", "text": "(nothing found)"}}, CausedBy: &msg})
		if err != nil {
			t.Fatal(err)
		}
		w.settle()
		rec.expect(t, "Primary", agents.PrimaryCharter, map[string]string{
			"MessagesThisTurn": fakecli.MessagesThisTurn,
			"PrimaryRetry":     fakecli.PrimaryRetry,
		})
		rec.expectLines(t, "Primary", agents.PrimaryCharter, lines{name: "MessageLine", pattern: fakecli.MessageLine, after: fakecli.MessagesThisTurn,
			want: map[string]func([]string) bool{
				"(user) line with the person's text": messageLine(msg.ID, "user", "你好 JUNK_PRIMARY"),
				"(external) line":                    messageLine(woke[0].ID, "external", ""),
				"(recall) line":                      messageLine(recalled.ID, "recall", ""),
			}})
	})

	// A child's turn, nudged once, then its worker's result, then its parent's turn.
	t.Run("manager", func(t *testing.T) {
		t.Parallel()
		w, rec := markerWorld(t)
		parent := m(w.k.CreateWorkItem(ctx, "parent", "test", kernel.CreateWorkItemOpts{}))
		child := m(w.k.CreateWorkItem(ctx, "child", "test", kernel.CreateWorkItemOpts{ParentID: parent.ID}))
		m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "ONLY_UNDERSTAND 写一个文件", Source: "connector:web", Target: child.ID}))
		w.settle()
		rec.expect(t, "Manager", agents.ManagerCharter, map[string]string{
			"MessagesThisTurn": fakecli.MessagesThisTurn,
			"ItemTitle":        fmt.Sprintf(fakecli.ItemTitle, child.Title),
			"ManagerNudge":     fakecli.ManagerNudge,
			"ChildItem":        fakecli.ChildItem,
			"WorkerResult":     fakecli.WorkerResult,
			"ResultOK":         fakecli.ResultOK,
			"ResultSummary":    fakecli.ResultSummary,
			"ChildrenSettled":  fakecli.ChildrenSettled,
		})
		var copied kernel.Event
		for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "user.message", ThreadID: child.ThreadID})) {
			copied = e
		}
		rec.expectLines(t, "Manager", agents.ManagerCharter, lines{name: "MessageLine", pattern: fakecli.MessageLine, after: fakecli.MessagesThisTurn,
			want: map[string]func([]string) bool{"line with the person's text": messageLine(copied.ID, "", "ONLY_UNDERSTAND 写一个文件")}})
	})

	t.Run("steered", func(t *testing.T) {
		t.Parallel()
		w, rec := markerWorld(t, "FAKEAGENT_WORKER_DELAY_MS=1500")
		item := m(w.k.CreateWorkItem(ctx, "steered", "test", kernel.CreateWorkItemOpts{}))
		m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "start", Source: "connector:web", Target: item.ID}))
		if err := w.rt.Drain(20); err != nil {
			t.Fatal(err)
		}
		waitRunning(t, w, item.ID)
		time.Sleep(200 * time.Millisecond)
		m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "also add a footer", Source: "connector:web", Target: item.ID}))
		w.settle()
		rec.expect(t, "Manager", agents.ManagerCharter, map[string]string{"SteerGiven": fakecli.SteerGiven})
	})

	// Outcomes a fake worker never ends with, posted as the pool would.
	t.Run("outcomes", func(t *testing.T) {
		t.Parallel()
		w, rec := markerWorld(t)
		item := m(w.k.CreateWorkItem(ctx, "outcomes", "test", kernel.CreateWorkItemOpts{}))
		// Recorded as running but no longer held by the pool: words for it come too late.
		if err := w.k.CreateExecution(ctx, "ex_late", item.ID, kernel.ManagerAddress(item.ID)); err != nil {
			t.Fatal(err)
		}
		if err := w.k.SetExecutionStatus(ctx, "ex_late", kernel.ExecRunning); err != nil {
			t.Fatal(err)
		}
		msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "also add a footer", Source: "connector:web", Target: item.ID}))
		if err := w.rt.Drain(20); err != nil {
			t.Fatal(err)
		}
		if err := w.k.SetExecutionStatus(ctx, "ex_late", kernel.ExecCancelled); err != nil {
			t.Fatal(err)
		}
		for id, p := range map[string]kernel.Payload{
			"ex_late":    {"cancelled": true},
			"ex_blocked": {"ok": true, "blocked": "which server?", "summary": "looked around"},
			"ex_failed":  {"ok": false, "error": "boom"},
		} {
			p["root"] = msg.ID
			if _, _, err := w.k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:worker", Kind: "execution.finished",
				Mailbox: kernel.ManagerAddress(item.ID), ThreadID: item.ThreadID, WorkItemID: item.ID, ExecutionID: id, Payload: p},
				AlwaysDeliver: true}); err != nil {
				t.Fatal(err)
			}
		}
		w.settle()
		rec.expect(t, "Manager", agents.ManagerCharter, map[string]string{
			"ResultCancelled": fakecli.ResultCancelled,
			"SteerLate":       fakecli.SteerLate,
			"ResultBlocked":   fakecli.ResultBlocked,
			"BlockedOn":       fakecli.BlockedOn,
			"ResultError":     fakecli.ResultError,
		})
	})

	// A replay: the second pass is shown what the first one promoted.
	t.Run("distiller", func(t *testing.T) {
		t.Parallel()
		w, rec := markerWorld(t)
		item := m(w.k.CreateWorkItem(ctx, "acme", "test", kernel.CreateWorkItemOpts{}))
		m(w.k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: item.ThreadID, WorkItemID: item.ID,
			Payload: kernel.Payload{"text": "任务约定：部署走main分支"}}))
		m(w.s.RunDistillation(ctx, 1))
		if err := w.k.ResetCursor(ctx, "distiller", 0); err != nil {
			t.Fatal(err)
		}
		m(w.s.RunDistillation(ctx, 1))
		rec.expect(t, "distiller", agents.DistillerCharter, map[string]string{
			"ExistingMemories": fakecli.ExistingMemories,
			"RecentEvents":     fakecli.RecentEvents,
		})
		rec.expectLines(t, "distiller", agents.DistillerCharter, lines{name: "DistilledItemMessage", pattern: fakecli.DistilledItemMessage + `(.*)`,
			after: fakecli.RecentEvents, want: map[string]func([]string) bool{
				"work item message": func(m []string) bool { return m[1] == item.ID && m[2] == "任务约定：部署走main分支" },
			}})
	})
}
