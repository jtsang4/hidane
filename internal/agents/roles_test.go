package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
	"github.com/jtsang4/hidane/internal/settings"
)

// The Primary and a Manager run with the CLI's tools in its bypass mode: no
// directory is fenced, their charter still replaces the CLI's prompt, each
// call passes the guard, and is recorded as a two-phase side effect.
func TestPrimaryAndManagerWorkWithTools(t *testing.T) {
	t.Parallel()
	calls := filepath.Join(t.TempDir(), "calls.jsonl")
	w := newWorld(t, settings.Claude, "FAKEAGENT_LOG="+calls)
	out := t.TempDir()
	looked := filepath.Join(out, "looked.txt")
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好 LOOK_FIRST: echo looked > " + looked + " END", Source: "connector:web"}))
	w.settle()
	if b, err := os.ReadFile(looked); err != nil || strings.TrimSpace(string(b)) != "looked" {
		t.Fatalf("the Primary changed a file outside any workspace: %q %v", b, err)
	}
	if w.lastReply(msg.ID) == "" {
		t.Fatal("and still answered with its effect list")
	}
	var phases []string
	for _, e := range w.events("") {
		if (e.Kind == "side_effect.intent" || e.Kind == "side_effect.result") && e.Source == "agent:primary" && e.ThreadID == "main" {
			phases = append(phases, e.Kind)
		}
	}
	if strings.Join(phases, ",") != "side_effect.intent,side_effect.result" {
		t.Fatalf("the Primary's tool call is a two-phase side effect: %v", phases)
	}

	// The deny list holds for it too. The probe is harmless even if it ran.
	probe := filepath.Join(out, "probe.txt")
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好 LOOK_FIRST: echo shutdown > " + probe + " END", Source: "connector:web"}))
	w.settle()
	if _, err := os.Stat(probe); err == nil {
		t.Fatal("a command on the deny list must be refused for the Primary")
	}

	item := m(w.k.CreateWorkItem(ctx, "look around", "test", kernel.CreateWorkItemOpts{}))
	checked := filepath.Join(out, "checked.txt")
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "LOOK_FIRST: echo checked > " + checked + " END 然后写个文件", Source: "connector:web", Target: item.ID}))
	w.settle()
	if _, err := os.Stat(checked); err != nil {
		t.Fatal("the Manager used its tools")
	}
	var managerTrail bool
	for _, e := range w.events("side_effect.intent") {
		if e.Source == "agent:manager" && e.WorkItemID == item.ID && e.ExecutionID == "" {
			managerTrail = true
		}
	}
	if !managerTrail {
		t.Fatal("the Manager's tool call is recorded on its work item")
	}

	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	roles := 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var inv invocation
		if json.Unmarshal([]byte(line), &inv) != nil {
			continue
		}
		args := strings.Join(inv.Args, " ")
		if strings.Contains(args, "Primary agent of hidane") || strings.Contains(args, "Manager of one work item") {
			roles++
			if !strings.Contains(args, "--system-prompt") || !strings.Contains(args, "bypassPermissions") || strings.Contains(args, `--tools `) {
				t.Fatalf("a reasoning role runs with tools in bypass mode, its charter as the prompt: %s", args)
			}
		}
	}
	if roles == 0 {
		t.Fatal("no Primary or Manager run was seen")
	}
}

// A rule that refuses the Primary's own call is a recorded fact, as a
// worker's refusal is.
func TestARefusedPrimaryCallIsRecorded(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	if err := guard.AddRule(w.k.GlobalPolicyPath(), guard.Rule{ID: "pol_t", Pattern: `forbidden\.txt`, Reason: "not that file"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "forbidden.txt")
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好 LOOK_FIRST: echo x > " + target + " END", Source: "connector:web"}))
	w.settle()
	if _, err := os.Stat(target); err == nil {
		t.Fatal("the rule must refuse the Primary's write")
	}
	blocked := w.events("policy.blocked")
	if len(blocked) != 1 || blocked[0].Source != "agent:primary" || blocked[0].ThreadID != "main" ||
		blocked[0].Payload.Str("reason") != "not that file" || blocked[0].Payload.Str("rule") == "" {
		t.Fatalf("policy.blocked: %+v", blocked)
	}
}

// A refusal is recorded when it happens: before the result of the call it
// stopped, and while the run still goes on — so the running card can show it.
// Codex shows no tool item for a refused call; a ticker catches that one.
func TestRefusalsAreRecordedWhileTheRunGoesOn(t *testing.T) {
	t.Parallel()
	for _, agent := range []string{settings.Claude, settings.Codex} {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, agent, "FAKEAGENT_WORKER_DELAY_MS=2500")
			if err := guard.AddRule(w.k.GlobalPolicyPath(), guard.Rule{ID: "pol_t", Pattern: `forbidden\.txt`, Reason: "not that file"}); err != nil {
				t.Fatal(err)
			}
			item := m(w.k.CreateWorkItem(ctx, "guarded", "test", kernel.CreateWorkItemOpts{}))
			m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "RUN: echo x > forbidden.txt", Source: "connector:web", Target: item.ID}))
			// The Manager's turn dispatches the worker; the run then goes on alone.
			if err := w.rt.Drain(50); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(30 * time.Second)
			var shown bool
			for !shown && time.Now().Before(deadline) {
				_, running, _ := w.k.ActiveExecutionFor(ctx, item.ID)
				if !running || len(w.events("policy.blocked")) == 0 {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				for _, card := range m(projections.BuildBoard(ctx, w.k, nil)) {
					if card.Item.ID == item.ID && card.Execution != nil && card.LastPolicyBlock != nil {
						shown = true
					}
				}
			}
			if !shown {
				t.Fatal("the refusal must be on record, and on the card, while the execution still runs")
			}
			w.settle()
			if agent == settings.Claude {
				kinds := w.kinds()
				blocked := indexOf(kinds, "policy.blocked", 0)
				if blocked < 0 || blocked > indexOf(kinds, "side_effect.result", indexOf(kinds, "side_effect.intent", 0)) {
					t.Fatalf("policy.blocked lands before the refused call's result: %v", kinds)
				}
			}
			if n := len(w.events("policy.blocked")); n != 1 {
				t.Fatalf("recorded once: %d", n)
			}
			if _, err := os.Stat(filepath.Join(item.Workspace, "forbidden.txt")); err == nil {
				t.Fatal("the refused write must not have happened")
			}
		})
	}
}
