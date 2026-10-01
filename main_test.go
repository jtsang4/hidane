package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/agentcli/fakecli"
)

// buildCLI builds the headless binary this package produces.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "hidane")
	cmd := exec.Command("go", "build", "-tags", "nogui", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func run(t *testing.T, bin, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HIDANE_HOME="+home, "HIDANE_LOGIN_SHELL=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hidane %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestCLIChatRunsTheWholeLoop(t *testing.T) {
	bin := buildCLI(t)
	fakes := fakecli.Dir(t)
	home := t.TempDir()
	roles := map[string]any{}
	for _, r := range []string{"primary", "manager", "worker", "distiller"} {
		roles[r] = map[string]any{"agent": "pi", "provider": "", "model": "", "effort": ""}
	}
	settings, _ := json.Marshal(map[string]any{"roles": roles, "binaries": map[string]string{
		"claude": filepath.Join(fakes, "claude"), "codex": filepath.Join(fakes, "codex"), "pi": filepath.Join(fakes, "pi")}})
	if err := os.WriteFile(filepath.Join(home, "settings.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}

	if out := run(t, bin, home, "agents"); strings.Count(out, " ok ") != 3 {
		t.Fatalf("agents:\n%s", out)
	}
	out := run(t, bin, home, "chat", "--timeout", "60", "写一个文件，内容是 cli-check")
	if !strings.Contains(out, "已完成") || !strings.Contains(out, "→ wi_") {
		t.Fatalf("chat output:\n%s", out)
	}
	items := run(t, bin, home, "items")
	if !strings.Contains(items, "[open]") {
		t.Fatalf("items:\n%s", items)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "workspaces", "wi_*", "result.txt"))
	if len(matches) != 1 {
		t.Fatalf("artifact: %v", matches)
	}
	if b, _ := os.ReadFile(matches[0]); !strings.Contains(string(b), "cli-check") {
		t.Fatalf("artifact content: %q", b)
	}
	events := run(t, bin, home, "events", "--tail", "60")
	for _, kind := range []string{"route.decision", "execution.finished", "agent.reply"} {
		if !strings.Contains(events, kind) {
			t.Fatalf("events missing %s:\n%s", kind, events)
		}
	}
	if out := run(t, bin, home, "log"); !strings.Contains(out, "# Worklog") {
		t.Fatalf("log:\n%s", out)
	}
	if out := run(t, bin, home, "model", "--ping", "--role", "worker"); !strings.Contains(out, "ping ok") {
		t.Fatalf("model --ping:\n%s", out)
	}
	if out := run(t, bin, home, "memories"); !strings.Contains(out, "(empty)") {
		t.Fatalf("memories:\n%s", out)
	}
	guard := exec.Command(bin, "guard", "--format", "codex")
	guard.Stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"sudo reboot"}}`)
	if err := guard.Run(); err == nil {
		t.Fatal("the guard subcommand refuses with a non-zero exit for codex")
	}
}
