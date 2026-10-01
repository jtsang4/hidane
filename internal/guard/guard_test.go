package guard_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/guard"
)

func TestBuiltinDenyList(t *testing.T) {
	for _, cmd := range []string{"sudo rm x", "rm -rf /", "rm -fr / ", "mkfs.ext4 /dev/sda", "dd if=/dev/zero of=x", "shutdown -h now", "git push origin main --force"} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, guard.Env{}); !d.Block || !d.Policy {
			t.Errorf("%q must be blocked", cmd)
		}
	}
	for _, cmd := range []string{"ls -la", "rm -rf ./build", "git push origin main"} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, guard.Env{}); d.Block {
			t.Errorf("%q must pass: %s", cmd, d.Reason)
		}
	}
}

func TestPolicyFilesApplyOutermostFirstAndToMutatingToolsByDefault(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	local := filepath.Join(dir, "local.json")
	_ = guard.WriteFile(global, guard.File{Rules: []guard.Rule{{ID: "pol_g", Pattern: `curl\s`, Reason: "no network"}}})
	_ = guard.WriteFile(local, guard.File{Rules: []guard.Rule{{ID: "pol_l", Pattern: `secrets/`, Reason: "no secrets", Tools: []string{"read", "write"}}}})
	env := guard.Env{PolicyFiles: []string{global, local}}
	d := guard.Evaluate(guard.Call{Tool: "bash", Subject: "curl https://x"}, env)
	if !d.Block || !strings.Contains(d.Reason, "pol_g") {
		t.Fatalf("global rule: %+v", d)
	}
	if d := guard.Evaluate(guard.Call{Tool: "read", Subject: "secrets/a"}, env); !d.Block {
		t.Fatal("explicit tools list applies to read")
	}
	if d := guard.Evaluate(guard.Call{Tool: "read", Subject: "curl x"}, env); d.Block {
		t.Fatal("rules without tools apply to mutating tools only")
	}
}

func TestPendingInputPausesChangesButNotReads(t *testing.T) {
	dir := t.TempDir()
	pending := filepath.Join(dir, "pending-input")
	_ = os.WriteFile(pending, []byte("new words"), 0o644)
	env := guard.Env{PendingInputFile: pending}
	if d := guard.Evaluate(guard.Call{Tool: "write", Subject: "a.txt"}, env); !d.Block || d.Policy {
		t.Fatalf("write must pause (not as a policy block): %+v", d)
	}
	if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: "git status"}, env); d.Block {
		t.Fatal("read-only commands continue")
	}
	if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: "ls > out.txt"}, env); !d.Block {
		t.Fatal("redirection is a change")
	}
}

func TestNormalizeAcrossCLIs(t *testing.T) {
	cases := []struct {
		tool  string
		input map[string]any
		want  guard.Call
	}{
		{"Bash", map[string]any{"command": "ls"}, guard.Call{Tool: "bash", Subject: "ls"}},
		{"Write", map[string]any{"file_path": "/w/a.txt"}, guard.Call{Tool: "write", Subject: "/w/a.txt"}},
		{"MultiEdit", map[string]any{"file_path": "b.go"}, guard.Call{Tool: "edit", Subject: "b.go"}},
		{"edit", map[string]any{"path": "c.md"}, guard.Call{Tool: "edit", Subject: "c.md"}},
		{"shell", map[string]any{"command": []any{"bash", "-lc", "echo hi"}}, guard.Call{Tool: "bash", Subject: "bash -lc echo hi"}},
		{"apply_patch", map[string]any{"patch": "*** Begin Patch\n*** Add File: secrets/x\n+a\n*** End Patch"}, guard.Call{Tool: "edit", Subject: "secrets/x"}},
	}
	for _, c := range cases {
		if got := guard.Normalize(c.tool, c.input); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.tool, got, c.want)
		}
	}
}

func hook(t *testing.T, format string, input any, env guard.Env) (int, string, string) {
	t.Helper()
	b, _ := json.Marshal(input)
	var out, errOut bytes.Buffer
	code := guard.RunHook(format, bytes.NewReader(b), &out, &errOut, env)
	return code, out.String(), errOut.String()
}

func TestHookProtocols(t *testing.T) {
	dir := t.TempDir()
	blocks := filepath.Join(dir, "blocks.jsonl")
	env := guard.Env{BlocksFile: blocks}
	deny := map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "sudo ls"}}
	allow := map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}

	code, out, _ := hook(t, "claude", deny, env)
	if code != 0 || !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("claude deny: %d %s", code, out)
	}
	if code, out, _ := hook(t, "claude", allow, env); code != 0 || out != "" {
		t.Fatalf("claude allow: %d %q", code, out)
	}
	code, _, stderr := hook(t, "codex", deny, env)
	if code != 2 || !strings.Contains(stderr, "blocked by hidane guard") {
		t.Fatalf("codex deny: %d %s", code, stderr)
	}
	code, out, _ = hook(t, "pi", map[string]any{"tool_name": "bash", "tool_input": map[string]any{"command": "sudo x"}}, env)
	var d guard.Decision
	_ = json.Unmarshal([]byte(out), &d)
	if code != 0 || !d.Block {
		t.Fatalf("pi deny: %d %s", code, out)
	}
	if got := guard.ReadBlocks(blocks); len(got) != 3 {
		t.Fatalf("every refusal is recorded for the execution: %+v", got)
	}
	var stdout, stderrBuf bytes.Buffer
	if code := guard.RunHook("codex", strings.NewReader("not json"), &stdout, &stderrBuf, env); code != 2 {
		t.Fatal("unreadable input must fail closed")
	}
}

func TestAnUnreadablePolicyFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "POLICY.json")
	_ = os.WriteFile(broken, []byte(`{"rules":[{"id":"x","pattern":"a\\.b","reason":"r"}`), 0o644)
	env := guard.Env{PolicyFiles: []string{filepath.Join(dir, "missing.json"), broken}}
	d := guard.Evaluate(guard.Call{Tool: "write", Subject: "anything"}, env)
	if !d.Block || !strings.Contains(d.Reason, "unreadable") {
		t.Fatalf("a broken policy file must refuse changes: %+v", d)
	}
	if d := guard.Evaluate(guard.Call{Tool: "read", Subject: "x"}, env); d.Block {
		t.Fatal("reads stay allowed")
	}
	if d := guard.Evaluate(guard.Call{Tool: "write", Subject: "x"}, guard.Env{PolicyFiles: []string{filepath.Join(dir, "missing.json")}}); d.Block {
		t.Fatal("a missing file is simply no rules")
	}
}

func TestWorkersStayInTheirWorkspace(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "workspaces", "wi_1")
	_ = os.MkdirAll(ws, 0o755)
	env := guard.Env{Workspace: ws, Protected: home}
	for _, c := range []guard.Call{
		{Tool: "write", Subject: filepath.Join(home, "POLICY.json")},
		{Tool: "edit", Subject: "../../settings.json"},
		{Tool: "write", Subject: "/etc/hosts"},
		{Tool: "bash", Subject: "cd " + home + " && echo hi > hello.txt"},
		{Tool: "bash", Subject: "rm " + filepath.Join(home, "hidane.db")},
	} {
		if d := guard.Evaluate(c, env); !d.Block || !d.Policy {
			t.Errorf("%+v must be refused", c)
		}
	}
	for _, c := range []guard.Call{
		{Tool: "write", Subject: "notes/a.md"},
		{Tool: "write", Subject: filepath.Join(ws, "b.txt")},
		{Tool: "bash", Subject: "cd " + ws + " && echo hi > hello.txt"},
		{Tool: "bash", Subject: "cat " + filepath.Join(home, "memory", "MEMORY.md")},
		{Tool: "edit", Subject: "*** Begin Patch"},
	} {
		if d := guard.Evaluate(c, env); d.Block {
			t.Errorf("%+v must pass: %s", c, d.Reason)
		}
	}
}
