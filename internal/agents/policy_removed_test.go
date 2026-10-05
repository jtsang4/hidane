package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A rule only matches and refuses: once the person removes it, the very action
// it refused runs.
func TestARefusedActionRunsOnceItsRuleIsRemoved(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	rule := guard.Rule{ID: "pol_norm", Pattern: `\brm\b`, Reason: "不允许删除文件"}
	if err := guard.AddRule(w.k.GlobalPolicyPath(), rule); err != nil {
		t.Fatal(err)
	}
	item := m(w.k.CreateWorkItem(ctx, "cleanup", "test", kernel.CreateWorkItemOpts{}))
	doomed := filepath.Join(item.Workspace, "doomed.txt")
	if err := os.WriteFile(doomed, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "RUN: rm doomed.txt", Source: "connector:web", Target: item.ID}))
	w.settle()
	blocked := w.events("policy.blocked")
	if len(blocked) != 1 || blocked[0].Payload.Str("rule") != rule.ID {
		t.Fatalf("the rule refuses the removal: %+v", blocked)
	}
	if _, err := os.Stat(doomed); err != nil {
		t.Fatal("the refused removal must not happen")
	}

	if removed, err := guard.RemoveRule(w.k.GlobalPolicyPath(), rule.ID); err != nil || !removed {
		t.Fatalf("remove the rule: %v %v", removed, err)
	}
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "RUN: rm doomed.txt", Source: "connector:web", Target: item.ID}))
	w.settle()
	if _, err := os.Stat(doomed); !os.IsNotExist(err) {
		t.Fatalf("without the rule the same removal runs: %v (kinds %v)", err, w.kinds())
	}
	if n := len(w.events("policy.blocked")); n != 1 {
		t.Fatalf("nothing more is refused: %d refusals", n)
	}
}
