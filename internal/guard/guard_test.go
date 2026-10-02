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
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, guard.Env{Workspace: t.TempDir()}); d.Block {
			t.Errorf("%q must pass: %s", cmd, d.Reason)
		}
	}
}

// A run without a workspace has nothing to be confined to: it may look, not change.
func TestNoWorkspaceRefusesChanges(t *testing.T) {
	for _, call := range []guard.Call{{Tool: "write", Subject: "a.txt"}, {Tool: "edit", Subject: "/tmp/a"}, {Tool: "bash", Subject: "echo x > /tmp/a"}} {
		if d := guard.Evaluate(call, guard.Env{}); !d.Block {
			t.Errorf("%+v must be refused without a workspace", call)
		}
	}
	for _, call := range []guard.Call{{Tool: "read", Subject: "a.txt"}, {Tool: "bash", Subject: "ls -la"}} {
		if d := guard.Evaluate(call, guard.Env{}); d.Block {
			t.Errorf("%+v only looks: %s", call, d.Reason)
		}
	}
}

func TestPolicyFilesApplyOutermostFirstAndToMutatingToolsByDefault(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.json")
	local := filepath.Join(dir, "local.json")
	_ = guard.WriteFile(global, guard.File{Rules: []guard.Rule{{ID: "pol_g", Pattern: `curl\s`, Reason: "no network"}}})
	_ = guard.WriteFile(local, guard.File{Rules: []guard.Rule{{ID: "pol_l", Pattern: `secrets/`, Reason: "no secrets", Tools: []string{"read", "write"}}}})
	env := guard.Env{PolicyFiles: []string{global, local}, Workspace: dir}
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
	// A shell command that only looks is a read, like the read tool.
	_ = guard.WriteFile(global, guard.File{Rules: []guard.Rule{{ID: "pol_g", Pattern: `forbidden\.txt`, Reason: "not that file"}}})
	if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: "ls -la forbidden.txt"}, env); d.Block {
		t.Fatalf("a read-only command is not a change: %s", d.Reason)
	}
	if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: "echo x > forbidden.txt"}, env); !d.Block {
		t.Fatal("writing the file is refused")
	}
}

func TestPendingInputPausesChangesButNotReads(t *testing.T) {
	dir := t.TempDir()
	pending := filepath.Join(dir, "pending-input")
	_ = os.WriteFile(pending, []byte("new words"), 0o644)
	env := guard.Env{PendingInputFile: pending, Workspace: dir}
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
	if got := guard.ReadBlocks(blocks); len(got) != 3 || strings.Contains(got[0].Reason, "nothing in this tool call ran") {
		t.Fatalf("every refusal is recorded for the execution, without the hint: %+v", got)
	}
	if !strings.Contains(stderr, "nothing in this tool call ran") {
		t.Fatalf("the model is told nothing ran: %s", stderr)
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
	env := guard.Env{PolicyFiles: []string{filepath.Join(dir, "missing.json"), broken}, Workspace: dir}
	d := guard.Evaluate(guard.Call{Tool: "write", Subject: "anything"}, env)
	if !d.Block || !strings.Contains(d.Reason, "unreadable") {
		t.Fatalf("a broken policy file must refuse changes: %+v", d)
	}
	if d := guard.Evaluate(guard.Call{Tool: "read", Subject: "x"}, env); d.Block {
		t.Fatal("reads stay allowed")
	}
	if d := guard.Evaluate(guard.Call{Tool: "write", Subject: "x"}, guard.Env{PolicyFiles: []string{filepath.Join(dir, "missing.json")}, Workspace: dir}); d.Block {
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

func TestReviewedGuardBypassesAreClosed(t *testing.T) {
	home, _ := os.UserHomeDir()
	data := filepath.Join(home, ".hidane-guard-test")
	ws := filepath.Join(t.TempDir(), "wi_1")
	_ = os.MkdirAll(filepath.Join(ws, ".hidane"), 0o755)
	env := guard.Env{Workspace: ws, Protected: data}
	refused := []guard.Call{
		{Tool: "write", Subject: "~/.hidane-guard-test/settings.json"},
		{Tool: "edit", Subject: "@/etc/hosts"},
		{Tool: "write", Subject: "~root/x"},
		{Tool: "write", Subject: ".hidane/POLICY.json"},
		{Tool: "write", Subject: filepath.Join(ws, ".hidane", "pending-input")},
		{Tool: "bash", Subject: "echo ok\nrm -rf " + data + "/hidane.db"},
		{Tool: "bash", Subject: "find " + data + " -delete"},
		{Tool: "bash", Subject: "echo $(rm -rf " + data + ")"},
		{Tool: "bash", Subject: "rm -rf ~/.hidane-guard-test/hidane.db"},
		{Tool: "bash", Subject: "rm .hidane/pending-input"},
		{Tool: "bash", Subject: "cat x > " + filepath.Join(ws, ".hidane", "POLICY.json")},
		{Tool: "edit", Subject: guard.Normalize("apply_patch", map[string]any{"patch": "*** Begin Patch\n*** Update File: a.txt\n*** Move to: /tmp/evil.txt\n*** End Patch"}).Subject},
	}
	for _, c := range refused {
		if d := guard.Evaluate(c, env); !d.Block {
			t.Errorf("%q (%s) must be refused", c.Subject, c.Tool)
		}
	}
	allowed := []guard.Call{
		{Tool: "write", Subject: "notes/hidane.md"},
		{Tool: "bash", Subject: "printf hi > " + filepath.Join(ws, "out.txt")},
		{Tool: "bash", Subject: "ls -la .hidane"},
		{Tool: "bash", Subject: "git branch -a"},
		{Tool: "bash", Subject: "find . -name '*.go'"},
	}
	for _, c := range allowed {
		if d := guard.Evaluate(c, env); d.Block {
			t.Errorf("%q must pass: %s", c.Subject, d.Reason)
		}
	}
	for cmd, ro := range map[string]bool{"git status": true, "git branch -D main": false, "tree -o x.txt": false, "find . -exec rm {} ;": false, "echo hi": true, "echo hi > f": false} {
		if guard.IsReadOnly(cmd) != ro {
			t.Errorf("IsReadOnly(%q) != %v", cmd, ro)
		}
	}
}

func TestShellWritesStayInTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	env := guard.Env{Workspace: ws, Protected: filepath.Join(t.TempDir(), "home")}
	refused := []string{
		"echo hi > /tmp/hidane-guard-x.log",
		"python3 -m http.server 8765 >/tmp/http_$PORT.log 2>&1 &",
		"cd /tmp && touch a",
		"cp a.txt /tmp/",
		"rm -rf /tmp/thing",
		"mkdir -p ~/elsewhere",
		"ls | tee /tmp/listing",
		`/bin/zsh -lc "echo x > /tmp/y"`,
		"date >> ../../outside.txt",
	}
	for _, cmd := range refused {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); !d.Block {
			t.Errorf("%q must be refused", cmd)
		}
	}
	allowed := []string{
		"echo hi > out.txt",
		"ls > /dev/null 2>&1",
		"cat /etc/hosts 2>/dev/null | grep localhost > hosts.txt",
		"cp /usr/share/dict/words .",
		"mkdir -p build && cd build && touch ok",
		"cd " + ws + " && echo hi > " + filepath.Join(ws, "a.txt"),
		"python3 script.py --out result.json",
		"FOO=1 ./run.sh 2>&1",
	}
	for _, cmd := range allowed {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); d.Block {
			t.Errorf("%q must pass: %s", cmd, d.Reason)
		}
	}
}

// Quoted text is data, not shell syntax; a chain of looks is still a look.
// Both were refused for real workers, costing them wasted tool calls.
func TestQuotesAndChainsAreReadAsTheShellReadsThem(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "wi_1")
	_ = os.MkdirAll(filepath.Join(ws, ".hidane"), 0o755)
	env := guard.Env{Workspace: ws}
	for _, cmd := range []string{
		`sed 's/=.*/=<set>/' .env.example`,
		`echo 'a > /etc/x'`,
		`grep -n "a;b > c" notes.md`,
		`cat a.txt; ls -R .hidane`,
		`ls && git status | head -3`,
	} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); d.Block {
			t.Errorf("%q must pass: %s", cmd, d.Reason)
		}
	}
	for _, cmd := range []string{
		`echo x > '/etc/x'`,
		`echo "a;b" > /tmp/z`,
		`ls; echo x > /tmp/y`,
		`cat a.txt; rm -rf .hidane`,
		`echo 'unclosed > /etc/x`,
	} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); !d.Block {
			t.Errorf("%q must be refused", cmd)
		}
	}
}
