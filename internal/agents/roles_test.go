package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// The Primary and a Manager run with the CLI's tools in its bypass mode: no
// directory is fenced, their charter still replaces the CLI's prompt, each
// call passes the guard, and is recorded as a two-phase side effect.
func TestPrimaryAndManagerWorkWithTools(t *testing.T) {
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
	w := newWorld(t, settings.Claude)
	if _, err := w.k.AddGlobalRule(`forbidden\.txt`, "not that file", nil); err != nil {
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
