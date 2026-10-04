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
		{"exec_command", map[string]any{"cmd": "ls", "workdir": "/w/sub"}, guard.Call{Tool: "bash", Subject: "ls", Dirs: []string{"/w/sub"}}},
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

// The workspace is where a worker starts, not a fence: it may change files
// anywhere — except what hidane itself runs on.
func TestWorkersKeepOffHidanesOwnData(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "workspaces", "wi_1")
	_ = os.MkdirAll(ws, 0o755)
	_ = os.MkdirAll(filepath.Join(home, "workspaces", "wi_2"), 0o755)
	env := guard.Env{Workspace: ws, Protected: home}
	for _, c := range []guard.Call{
		{Tool: "write", Subject: filepath.Join(home, "POLICY.json")},
		{Tool: "edit", Subject: "../../settings.json"},
		{Tool: "write", Subject: "../wi_2/notes.md"},
		{Tool: "bash", Subject: "cd " + home + " && echo hi > hello.txt"},
		{Tool: "bash", Subject: "rm " + filepath.Join(home, "hidane.db")},
		{Tool: "bash", Subject: "cd ../wi_2 && touch x"},
	} {
		if d := guard.Evaluate(c, env); !d.Block || !d.Policy {
			t.Errorf("%+v must be refused", c)
		}
	}
	outside := t.TempDir()
	for _, c := range []guard.Call{
		{Tool: "write", Subject: "notes/a.md"},
		{Tool: "write", Subject: filepath.Join(ws, "b.txt")},
		{Tool: "write", Subject: filepath.Join(outside, "c.txt")},
		{Tool: "edit", Subject: "~/.config/some-tool/config.toml"},
		{Tool: "bash", Subject: "cd " + ws + " && echo hi > hello.txt"},
		{Tool: "bash", Subject: "cd " + outside + " && git init && echo hi > README.md"},
		{Tool: "bash", Subject: "mkdir -p ~/elsewhere && cp a.txt /tmp/"},
		{Tool: "bash", Subject: "cat " + filepath.Join(home, "memory", "MEMORY.md")},
		{Tool: "bash", Subject: "find " + home + " -name x 2>/dev/null"},
		{Tool: "bash", Subject: "cat " + filepath.Join(home, "POLICY.json") + " 2>&1"},
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
		{Tool: "edit", Subject: "@" + data + "/POLICY.json"},
		{Tool: "write", Subject: "~root/x"},
		{Tool: "write", Subject: ".hidane/POLICY.json"},
		{Tool: "write", Subject: filepath.Join(ws, ".hidane", "pending-input")},
		{Tool: "bash", Subject: "echo ok\nrm -rf " + data + "/hidane.db"},
		{Tool: "bash", Subject: "find " + data + " -delete"},
		{Tool: "bash", Subject: "echo $(rm -rf " + data + ")"},
		{Tool: "bash", Subject: "rm -rf ~/.hidane-guard-test/hidane.db"},
		{Tool: "bash", Subject: "rm .hidane/pending-input"},
		{Tool: "bash", Subject: "cat x > " + filepath.Join(ws, ".hidane", "POLICY.json")},
		{Tool: "edit", Subject: guard.Normalize("apply_patch", map[string]any{"patch": "*** Begin Patch\n*** Update File: a.txt\n*** Move to: " + data + "/evil.txt\n*** End Patch"}).Subject},
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
	for cmd, ro := range map[string]bool{"cat a.txt 2>/dev/nullx": false, "git status": true, "git branch -D main": false, "tree -o x.txt": false, "find . -exec rm {} ;": false, "echo hi": true, "echo hi > f": false} {
		if guard.IsReadOnly(cmd) != ro {
			t.Errorf("IsReadOnly(%q) != %v", cmd, ro)
		}
	}
}

func TestShellWritesKeepOffHidanesOwnData(t *testing.T) {
	data := t.TempDir()
	ws := filepath.Join(data, "workspaces", "wi_1")
	_ = os.MkdirAll(ws, 0o755)
	env := guard.Env{Workspace: ws, Protected: data}
	refused := []string{
		"echo hi > " + data + "/x.log",
		"python3 -m http.server 8765 >" + data + "/http_$PORT.log 2>&1 &",
		"cd " + data + " && touch a",
		"cp a.txt " + data + "/",
		"rm -rf " + data + "/thing",
		"ls | tee " + data + "/listing",
		`/bin/zsh -lc "echo x > ` + data + `/y"`,
		"date >> ../../outside.txt",
		"cd $TMPDIR && echo x > y",
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
		"mkdir -p sub && cd sub && touch x && cd .. && touch y",
		"echo hi > /tmp/hidane-guard-x.log",
		"cd /tmp && touch a",
		"rm -rf /tmp/thing",
		"mkdir -p ~/elsewhere",
		"ls | tee /tmp/listing",
		"date >> ../../../outside.txt",
	}
	for _, cmd := range allowed {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); d.Block {
			t.Errorf("%q must pass: %s", cmd, d.Reason)
		}
	}
	// A shell the CLI reports in hidane's data directory writes there.
	if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: "echo x > y", Dirs: []string{data}}, env); !d.Block {
		t.Error("a relative write from hidane's data directory must be refused")
	}
}

func mustHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	return home
}

// Quoted text is data, not shell syntax; a chain of looks is still a look.
// Both were refused for real workers, costing them wasted tool calls.
func TestQuotesAndChainsAreReadAsTheShellReadsThem(t *testing.T) {
	data := t.TempDir()
	ws := filepath.Join(data, "wi_1")
	_ = os.MkdirAll(filepath.Join(ws, ".hidane"), 0o755)
	env := guard.Env{Workspace: ws, Protected: data}
	for _, cmd := range []string{
		`sed 's/=.*/=<set>/' .env.example`,
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
	} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); d.Block {
			t.Errorf("%q must pass: %s", cmd, d.Reason)
		}
	}
	// The writes these hide must still be read off them: aimed at hidane's
	// own data, every one is refused.
	for _, cmd := range []string{
		`echo x > '` + data + `/x'`,
		`echo "a;b" > ` + data + `/z`,
		`ls; echo x > ` + data + `/y`,
		`cat a.txt; rm -rf .hidane`,
		`echo 'unclosed > ` + data + `/x`,
		`ls 2>&1 > ` + data + `/out`,
		`cp -t ` + data + ` a.txt`,
		`cp --target-directory=` + data + ` a.txt`,
		`mv -t` + data + ` a.txt`,
		`true && echo x > ` + data + `/y`,
		`sort -o ` + data + `/y notes.md`,
		`sort --output=` + data + `/y notes.md`,
		`uniq notes.md ` + data + `/y`,
	} {
		if d := guard.Evaluate(guard.Call{Tool: "bash", Subject: cmd}, env); !d.Block {
			t.Errorf("%q must be refused", cmd)
		}
	}
}

// settings.json holds the provider keys: not even a read, by any route.
func TestSettingsFileIsOffLimits(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "workspaces", "wi_1")
	_ = os.MkdirAll(ws, 0o755)
	env := guard.Env{Workspace: ws, Protected: home}
	settingsFile := filepath.Join(home, "settings.json")
	for _, c := range []guard.Call{
		{Tool: "read", Subject: settingsFile},
		{Tool: "read", Subject: "../../settings.json"},
		{Tool: "bash", Subject: "cat " + settingsFile},
		{Tool: "bash", Subject: "cat ../../settings.json 2>/dev/null"},
		{Tool: "grep", Subject: "apiKey " + settingsFile},
		// Relative to where the command really is: a real worker read the key
		// with the first of these.
		{Tool: "bash", Subject: "mkdir -p sub && cd sub && cat ../../../settings.json"},
		{Tool: "bash", Subject: "pushd sub >/dev/null; cat ../../../settings.json"},
		{Tool: "bash", Subject: "cat ../../../settings.json", Dirs: []string{filepath.Join(ws, "sub")}},
		{Tool: "read", Subject: "../../../settings.json", Dirs: []string{filepath.Join(ws, "sub")}},
		{Tool: "bash", Subject: "cd ~ && cat " + strings.TrimPrefix(settingsFile, mustHome(t)+"/")},
		{Tool: "bash", Subject: "cd $SOMEWHERE && cat settings.json"},
	} {
		if d := guard.Evaluate(c, env); !d.Block || !strings.Contains(d.Reason, "API keys") {
			t.Errorf("%+v must be refused: %+v", c, d)
		}
	}
	for _, c := range []guard.Call{
		{Tool: "read", Subject: "settings.json"},
		{Tool: "bash", Subject: "cat config/settings.json"},
		{Tool: "bash", Subject: "cat " + filepath.Join(home, "memory", "MEMORY.md")},
		{Tool: "bash", Subject: "cd sub && cat ../settings.json"},
		{Tool: "bash", Subject: "cd sub && cd .. && cat settings.json"},
	} {
		if d := guard.Evaluate(c, env); d.Block {
			t.Errorf("%+v is the workspace's own file: %s", c, d.Reason)
		}
	}
	// The directory a CLI reports in its hook input counts as well.
	input := map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "cat ../../../settings.json"}, "cwd": filepath.Join(ws, "sub")}
	if code, _, stderr := hook(t, "codex", input, env); code != 2 || !strings.Contains(stderr, "API keys") {
		t.Errorf("the hook's cwd must be used: %d %s", code, stderr)
	}
}

// Relative paths are relative to where the worker runs; a directory the
// person lent stays writable even inside hidane's data directory, while the
// rest of that directory is still off limits.
func TestLentDirectoriesAndTheWorkingDirectory(t *testing.T) {
	data := t.TempDir()
	ws := filepath.Join(data, "workspaces", "wi_1")
	lent := t.TempDir()
	lentInside := filepath.Join(data, "lent")
	other := t.TempDir()
	tree := filepath.Join(ws, "blog")
	for _, d := range []string{tree, lentInside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := guard.Env{Workspace: ws, Writable: []string{lent, lentInside}, Cwd: tree, Protected: data}
	for _, call := range []guard.Call{
		{Tool: "write", Subject: filepath.Join(lent, "a.txt")},
		{Tool: "write", Subject: filepath.Join(lentInside, "a.txt")},
		{Tool: "write", Subject: "src/a.txt"},
		{Tool: "write", Subject: "../notes.md"},
		{Tool: "write", Subject: filepath.Join(other, "a.txt")},
		{Tool: "bash", Subject: "echo x > " + filepath.Join(lent, "b.txt")},
		{Tool: "bash", Subject: "cd " + lent + " && touch c.txt"},
		{Tool: "bash", Subject: "echo x > " + filepath.Join(other, "b.txt")},
	} {
		if d := guard.Evaluate(call, env); d.Block {
			t.Errorf("%+v may be changed: %s", call, d.Reason)
		}
	}
	for _, call := range []guard.Call{
		{Tool: "write", Subject: filepath.Join(data, "POLICY.json")},
		{Tool: "write", Subject: "../../escape.txt"},
		{Tool: "bash", Subject: "echo x > " + filepath.Join(data, "b.txt")},
	} {
		if d := guard.Evaluate(call, env); !d.Block {
			t.Errorf("%+v is hidane's own data: it must be refused", call)
		}
	}
	// Round trip through the environment the CLIs carry.
	for _, kv := range env.Vars() {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	got := guard.EnvFromOS()
	if got.Cwd != tree || len(got.Writable) != 2 || got.Writable[0] != lent {
		t.Fatalf("env round trip: %+v", got)
	}
}

// The person's own checkout of a repo the task works on in a worktree stays
// theirs: changing it is refused, working in the worktree is not.
func TestTheReservedCheckoutStaysThePersons(t *testing.T) {
	data := t.TempDir()
	ws := filepath.Join(data, "workspaces", "wi_1")
	tree := filepath.Join(ws, "blog")
	repo := t.TempDir()
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	env := guard.Env{Workspace: ws, Reserved: []string{repo}, Cwd: tree, Protected: data}
	for _, call := range []guard.Call{
		{Tool: "write", Subject: filepath.Join(repo, "NOTES.md")},
		{Tool: "bash", Subject: "echo hi > " + filepath.Join(repo, "NOTES.md")},
		{Tool: "bash", Subject: "cd " + repo + " && git commit -am wip"},
		{Tool: "bash", Subject: "git commit -am wip", Dirs: []string{repo}},
	} {
		if d := guard.Evaluate(call, env); !d.Block || !strings.Contains(d.Reason, "worktree") {
			t.Errorf("%+v changes the person's checkout: %+v", call, d)
		}
	}
	for _, call := range []guard.Call{
		{Tool: "write", Subject: "NOTES.md"},
		{Tool: "bash", Subject: "git add -A && git commit -m wip"},
		{Tool: "bash", Subject: "cat " + filepath.Join(repo, "README.md")},
		{Tool: "write", Subject: filepath.Join(t.TempDir(), "elsewhere.txt")},
	} {
		if d := guard.Evaluate(call, env); d.Block {
			t.Errorf("%+v is allowed: %s", call, d.Reason)
		}
	}
}
