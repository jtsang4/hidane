package agents

import (
	"testing"

	"github.com/jtsang4/hidane/internal/agentcli"
)

// A turn that only looked answers; one that changed something reports work.
func TestChangesTellsLookingFromChanging(t *testing.T) {
	for _, c := range []struct {
		tool, detail string
		want         bool
	}{
		{"Read", `{"file_path":"/w/a.md"}`, false},
		{"Bash", `{"command":"ls -la && cat a.md"}`, false},
		{"Bash", `{"command":"echo hi > a.md"}`, true},
		{"Write", `{"file_path":"/w/a.md","content":"x"}`, true},
		{"Write", `{"file_path":"/w/a.md","content":"clipped`, true},
		{"Edit", `{"file_path":"/w/a.md"}`, true},
		{"bash", "git status", false},
		{"bash", "sed -i s/a/b/ a.md", true},
		{"edit", `[{"path":"/w/a.md","kind":"update"}]`, true},
		{"Grep", `{"pattern":"x"}`, false},
	} {
		if got := changes(agentcli.ToolEvent{Phase: "start", Tool: c.tool, Detail: c.detail}); got != c.want {
			t.Errorf("%s %s: %v, want %v", c.tool, c.detail, got, c.want)
		}
	}
}
