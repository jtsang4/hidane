package guard_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	// A shell command that only looks is a read, like the read tool.
	_ = guard.WriteFile(global, guard.File{Rules: []guard.Rule{{ID: "pol_g", Pattern: `forbidden\.txt`, Reason: "not that file"}}})
	for _, cmd := range []string{"ls -la forbidden.txt", "cat forbidden.txt 2>/dev/null", "grep keep forbidden.txt 2>&1 | wc -l",
		// A real claude worker's look, refused while cd counted as a change.
		"cd " + dir + ` && echo "--- hello.txt"; cat hello.txt; echo "--- forbidden.txt"; cat forbidden.txt`} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); d.Block {
			t.Fatalf("%q is a read, not a change: %s", cmd, d.Reason)
		}
	}
	for _, cmd := range []string{"echo x > forbidden.txt", "cd " + dir + " && echo x > forbidden.txt"} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); !d.Block {
			t.Fatalf("%q writes the file and is refused", cmd)
		}
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
		{"exec_command", map[string]any{"cmd": "ls", "workdir": "/w/sub"}, guard.Call{Tool: "bash", Subject: "ls"}},
	}
	for _, c := range cases {
		if got := guard.Normalize(c.tool, c.input); !reflect.DeepEqual(got, c.want) {
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

// The workspace is where an agent starts, not a fence: hidane refuses only
// the deny list and the person's own rules, wherever a call reaches.
func TestNothingIsFenced(t *testing.T) {
	home := t.TempDir()
	for _, c := range []guard.Call{
		{Tool: "write", Subject: filepath.Join(home, "workspaces", "wi_2", "notes.md")},
		{Tool: "write", Subject: filepath.Join(t.TempDir(), "elsewhere.txt")},
		{Tool: "edit", Subject: "~/.config/some-tool/config.toml"},
		{Tool: "read", Subject: filepath.Join(home, "settings.json")},
		{Tool: "bash", Subject: "cd " + home + " && echo hi > hello.txt"},
		{Tool: "bash", Subject: "cat ../../settings.json"},
		{Tool: "bash", Subject: "echo x > .hidane/notes"},
		{Tool: "bash", Subject: "git commit -am wip"},
	} {
		if d := guard.Evaluate(c, guard.Env{}); d.Block {
			t.Errorf("%+v must pass: %s", c, d.Reason)
		}
	}
}

// Whether a command only looks decides whether rules without tools apply and
// whether pending input pauses it. Quoted text is data, not shell syntax; a
// chain of looks is still a look; a write hidden in a chain is a change.
func TestReadOnlyCommands(t *testing.T) {
	for _, cmd := range []string{
		`echo 'a > /etc/x'`,
		`grep -n "a;b > c" notes.md`,
		`cat a.txt; ls -R .hidane`,
		`ls && git status | head -3`,
		`ls .hidane 2>&1`,
		`find . -name x 2>/dev/null`,
		`grep keep notes.md 2>&1 | wc -l`,
		`cat notes.md >/dev/null 2>&1`,
		`ls victim.txt 2>&1 || true`,
		`test -f notes.md && echo yes || echo no`,
		`[ -d src ] && ls src`,
		"git status",
		"git branch -a",
		"echo hi",
	} {
		if !guard.IsReadOnly(cmd) {
			t.Errorf("%q only looks", cmd)
		}
	}
	for _, cmd := range []string{
		`echo x > y`,
		`echo "a;b" > z`,
		`ls; echo x > y`,
		`cat a.txt; rm -rf .hidane`,
		`echo 'unclosed > x`,
		`ls 2>&1 > out`,
		"cat a.txt 2>/dev/nullx",
		"git branch -D main",
		"tree -o x.txt",
		"find . -exec rm {} ;",
		"echo ok\nrm x",
		"echo $(rm -rf x)",
	} {
		if guard.IsReadOnly(cmd) {
			t.Errorf("%q changes something", cmd)
		}
	}
}
