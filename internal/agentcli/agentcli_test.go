package agentcli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agentcli/fakecli"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/settings"
)

// The test binary doubles as the `hidane guard` hook command.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "guard" {
		format := "claude"
		if len(os.Args) > 3 {
			format = os.Args[3]
		}
		os.Exit(guard.RunHook(format, os.Stdin, os.Stdout, os.Stderr, guard.EnvFromOS()))
	}
	os.Exit(m.Run())
}

const workerCharter = "You are a Worker execution of hidane running inside a work item workspace."

type harness struct {
	t   *testing.T
	l   *agentcli.Launcher
	log string
	cwd string
}

func newHarness(t *testing.T, extraEnv ...string) *harness {
	t.Helper()
	dir := fakecli.Dir(t)
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "invocations.jsonl")
	self, _ := os.Executable()
	env := agentcli.WithoutVars(os.Environ(), "PATH")
	env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKEAGENT_LOG="+logPath)
	env = append(env, extraEnv...)
	l := &agentcli.Launcher{
		Binary:       func(agent string) (string, error) { return agentcli.LookPath(agent, env) },
		GuardCommand: self,
		RuntimeDir:   filepath.Join(tmp, "runtime"),
		Env:          env,
	}
	if err := l.EnsureRuntimeFiles(); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(tmp, "ws")
	_ = os.MkdirAll(cwd, 0o755)
	return &harness{t: t, l: l, log: logPath, cwd: cwd}
}

type invocation struct {
	Kind string            `json:"kind"`
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
}

func (h *harness) invocations() []invocation {
	b, _ := os.ReadFile(h.log)
	var out []invocation
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var inv invocation
		if json.Unmarshal([]byte(line), &inv) == nil {
			out = append(out, inv)
		}
	}
	return out
}

func (h *harness) req(prompt string) agentcli.Request {
	return agentcli.Request{Prompt: prompt, Cwd: h.cwd, Timeout: 20 * time.Second, SessionDir: filepath.Join(h.cwd, ".sessions"),
		Env: guard.Env{Workspace: h.cwd}.Vars()}
}

func TestReasoningCallsOnEveryCLI(t *testing.T) {
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			threads := filepath.Join(t.TempDir(), "threads.jsonl")
			h := newHarness(t, "FAKEAGENT_THREAD_LOG="+threads)
			var mu sync.Mutex
			var streamed strings.Builder
			req := h.req("ping")
			req.SystemPrompt = "This is a connectivity check. Reply with exactly: OK"
			req.OnText = func(d string) { mu.Lock(); streamed.WriteString(d); mu.Unlock() }
			res := agentcli.Call(context.Background(), h.l, agent, req)
			if !res.OK || res.Text != "OK" {
				t.Fatalf("result: %+v", res)
			}
			if streamed.String() != "OK" {
				t.Fatalf("streamed text: %q", streamed.String())
			}
			if res.SessionID == "" {
				t.Fatal("a session id is needed to resume")
			}
			inv := h.invocations()[0]
			joined := strings.Join(inv.Args, " ")
			if agent != "codex" && !strings.Contains(joined, "--system-prompt This is a connectivity check") {
				t.Fatalf("a reasoning role's charter replaces the coding-agent prompt: %v", inv.Args)
			}
			switch agent {
			case "claude":
				if !strings.Contains(joined, "--tools  ") && !strings.HasSuffix(joined, "--tools ") {
					found := false
					for i, a := range inv.Args {
						if a == "--tools" && i+1 < len(inv.Args) && inv.Args[i+1] == "" {
							found = true
						}
					}
					if !found {
						t.Fatalf("reasoning roles run without tools: %v", inv.Args)
					}
				}
				if !slices.Contains(inv.Args, "--strict-mcp-config") {
					t.Fatalf("nor the MCP servers of the person's own config: %v", inv.Args)
				}
			case "codex":
				if !strings.Contains(joined, `sandbox_mode="read-only"`) || !strings.Contains(joined, "features.shell_tool=false") ||
					!strings.Contains(joined, `web_search="disabled"`) || strings.Contains(joined, "developer_instructions") {
					t.Fatalf("reasoning roles run read-only, without tools: %v", inv.Args)
				}
				// The fake answers OK only when it got the charter: through
				// baseInstructions, which replaces codex's own coding-agent prompt.
				b, _ := os.ReadFile(threads)
				var params struct {
					BaseInstructions string         `json:"baseInstructions"`
					Config           map[string]any `json:"config"`
				}
				if json.Unmarshal(b, &params) != nil || params.BaseInstructions != req.SystemPrompt || params.Config["mcp_servers.fake_docs.enabled"] != false {
					t.Fatalf("the charter replaces the base instructions and the person's MCP servers are off: %s", b)
				}
			case "pi":
				if !strings.Contains(joined, "--no-tools") || inv.Env["PI_OFFLINE"] != "1" {
					t.Fatalf("pi reasoning flags: %v %v", inv.Args, inv.Env)
				}
			}
			if _, set := inv.Env["CLAUDECODE"]; set {
				t.Fatal("parent session markers must not leak into the CLI")
			}
		})
	}
}

func TestWorkersPassTheGuardOnEveryCLI(t *testing.T) {
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			h := newHarness(t, "CLAUDECODE=1")
			blocks := filepath.Join(h.cwd, ".hidane", "blocks.jsonl")
			_ = os.MkdirAll(filepath.Dir(blocks), 0o755)
			env := guard.Env{BlocksFile: blocks, Workspace: h.cwd}
			var mu sync.Mutex
			var tools []agentcli.ToolEvent
			req := h.req("WRITE result.txt: hello world\nRUN: sudo rm -rf /tmp/x")
			req.SystemPrompt = workerCharter
			req.Tools = true
			req.Env = env.Vars()
			req.OnTool = func(e agentcli.ToolEvent) { mu.Lock(); tools = append(tools, e); mu.Unlock() }
			res := agentcli.Call(context.Background(), h.l, agent, req)
			if !res.OK {
				t.Fatalf("run failed: %+v", res)
			}
			b, err := os.ReadFile(filepath.Join(h.cwd, "result.txt"))
			if err != nil || strings.TrimSpace(string(b)) != "hello world" {
				t.Fatalf("worker output: %q %v", b, err)
			}
			if !strings.Contains(res.Text, "blocked by hidane guard") {
				t.Fatalf("the sudo call must be refused by the guard: %s", res.Text)
			}
			got := guard.ReadBlocks(blocks)
			if len(got) != 1 {
				t.Fatalf("refusal recorded for the execution: %+v", got)
			}
			starts, errs := 0, 0
			for _, e := range tools {
				if e.Phase == "start" {
					starts++
				}
				if e.Phase == "end" && e.IsError {
					errs++
				}
			}
			wantStarts, wantErrs := 2, 1
			if agent == "codex" {
				// app-server shows no tool item for a call its hook refused.
				wantStarts, wantErrs = 1, 0
			}
			if starts != wantStarts || errs != wantErrs || res.ToolCalls != wantStarts {
				t.Fatalf("tool events: %+v (calls %d)", tools, res.ToolCalls)
			}

		})
	}
}

func TestProviderInjectionKeepsKeysOffTheCommandLine(t *testing.T) {
	p := &settings.Provider{ID: "ds", Label: "DeepSeek", AnthropicBaseURL: "https://api.deepseek.com/anthropic",
		OpenAIBaseURL: "https://gw.example.com/v1", PiProvider: "deepseek", APIKey: "sk-secret-123456"}
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			h := newHarness(t, "ANTHROPIC_API_KEY=inherited-should-vanish", "ANTHROPIC_BASE_URL=http://inherited.example")
			req := h.req("ping")
			req.SystemPrompt = "connectivity check"
			req.Provider = p
			req.Model = "deepseek-v4-pro"
			req.Effort = "high"
			res := agentcli.Call(context.Background(), h.l, agent, req)
			if !res.OK {
				t.Fatalf("%+v", res)
			}
			inv := h.invocations()[0]
			args := strings.Join(inv.Args, " ")
			switch agent {
			case "claude":
				if inv.Env["ANTHROPIC_BASE_URL"] != p.AnthropicBaseURL || inv.Env["ANTHROPIC_AUTH_TOKEN"] != p.APIKey || inv.Env["ANTHROPIC_MODEL"] != "deepseek-v4-pro" {
					t.Fatalf("claude env: %+v", inv.Env)
				}
				if strings.Contains(args, p.APIKey) {
					t.Fatal("the key must not be on the command line")
				}
				if !strings.Contains(args, "--effort high") || !strings.Contains(args, "--model deepseek-v4-pro") {
					t.Fatalf("claude args: %s", args)
				}
			case "codex":
				if inv.Env["HIDANE_CODEX_API_KEY"] != p.APIKey || strings.Contains(args, p.APIKey) {
					t.Fatalf("codex key handling: %+v %s", inv.Env, args)
				}
				for _, want := range []string{`model_provider="hidane"`, `model_providers.hidane.base_url="https://gw.example.com/v1"`,
					`model_providers.hidane.wire_api="responses"`, `model_reasoning_effort="high"`, `model="deepseek-v4-pro"`} {
					if !strings.Contains(args, want) {
						t.Fatalf("codex args missing %s: %s", want, args)
					}
				}
			case "pi":
				if _, inherited := inv.Env["ANTHROPIC_BASE_URL"]; inherited {
					t.Fatal("an inherited endpoint must not redirect the configured provider")
				}
				if !strings.Contains(args, "--provider deepseek") || !strings.Contains(args, "--model deepseek-v4-pro") || !strings.Contains(args, "--thinking high") {
					t.Fatalf("pi args: %s", args)
				}
			}
		})
	}
}

func TestSteeringReachesARunningAgent(t *testing.T) {
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			h := newHarness(t, "FAKEAGENT_DELAY_MS=400")
			consumed := make(chan struct{}, 4)
			req := h.req("WRITE a.txt: first")
			req.SystemPrompt = workerCharter
			req.Tools = true
			req.OnSteerConsumed = func() { consumed <- struct{}{} }
			run, err := h.l.Start(context.Background(), agent, req)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			if !run.Steer("WRITE b.txt: second") {
				t.Fatal("a running agent must accept steering")
			}
			res := run.Wait()
			if !res.OK {
				t.Fatalf("%+v", res)
			}
			select {
			case <-consumed:
			default:
				t.Fatal("steered input was never reported as consumed")
			}
			if agent != "pi" {
				if _, err := os.Stat(filepath.Join(h.cwd, "b.txt")); err != nil {
					t.Fatalf("steered instruction was not carried out: %v (%s)", err, res.Text)
				}
			}
			if agent == "codex" {
				// Steered into the running turn: one process, one final answer for both.
				if invs := h.invocations(); len(invs) != 1 || invs[0].Args[0] != "app-server" {
					t.Fatalf("codex runs one app-server: %+v", invs)
				}
				if !strings.Contains(res.Text, "a.txt") || !strings.Contains(res.Text, "b.txt") {
					t.Fatalf("the steered words joined the running turn: %s", res.Text)
				}
			}
			if run.Steer("too late") {
				t.Fatal("a finished run must refuse steering")
			}
		})
	}
}

// Real claude folds a message steered in mid-turn into the running turn: one
// result for two messages. The run must still end.
func TestClaudeSteerAbsorbedIntoTheRunningTurn(t *testing.T) {
	h := newHarness(t, "FAKEAGENT_DELAY_MS=400", "FAKEAGENT_CLAUDE_ABSORB=1")
	consumed := make(chan struct{}, 4)
	req := h.req("WRITE a.txt: first")
	req.SystemPrompt = workerCharter
	req.Tools = true
	req.Timeout = 10 * time.Second
	req.OnSteerConsumed = func() { consumed <- struct{}{} }
	run, err := h.l.Start(context.Background(), "claude", req)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if !run.Steer("WRITE b.txt: second") {
		t.Fatal("a running agent must accept steering")
	}
	res := run.Wait()
	if !res.OK {
		t.Fatalf("%+v", res)
	}
	if len(consumed) != 1 {
		t.Fatal("steered input was never reported as consumed")
	}
	if _, err := os.Stat(filepath.Join(h.cwd, "b.txt")); err != nil {
		t.Fatalf("steered instruction was not carried out: %v (%s)", err, res.Text)
	}
}

// app-server skips a hook it does not trust without a word: a tool call the
// guard never saw must stop the run, not pass unchecked.
func TestCodexStopsWhenTheGuardDidNotRun(t *testing.T) {
	h := newHarness(t, "FAKEAGENT_CODEX_SKIP_HOOKS=1")
	req := h.req("RUN: sleep 2\nWRITE late.txt: never")
	req.SystemPrompt = workerCharter
	req.Tools = true
	res := agentcli.Call(context.Background(), h.l, "codex", req)
	if res.OK || !strings.Contains(res.Error, "without asking the hidane guard") {
		t.Fatalf("an unguarded tool call must end the run: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(h.cwd, "late.txt")); err == nil {
		t.Fatal("nothing after the unguarded call may run")
	}
}

// Words that reach a run before its first turn has started go into that turn.
func TestCodexSteerBeforeTheTurnStarts(t *testing.T) {
	h := newHarness(t, "FAKEAGENT_DELAY_MS=300")
	consumed := make(chan struct{}, 4)
	req := h.req("WRITE a.txt: first")
	req.SystemPrompt = workerCharter
	req.Tools = true
	req.OnSteerConsumed = func() { consumed <- struct{}{} }
	run, err := h.l.Start(context.Background(), "codex", req)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Steer("WRITE b.txt: second") {
		t.Fatal("a starting run must accept steering")
	}
	res := run.Wait()
	if !res.OK || !strings.Contains(res.Text, "b.txt") || len(consumed) != 1 {
		t.Fatalf("%+v (consumed %d)", res, len(consumed))
	}
}

// Words the running turn no longer takes open the next turn on the same
// thread, in the same run, instead of being lost.
func TestCodexSteerThatMissesTheTurnOpensTheNext(t *testing.T) {
	h := newHarness(t, "FAKEAGENT_DELAY_MS=300", "FAKEAGENT_CODEX_REFUSE_STEER=1")
	consumed := make(chan struct{}, 4)
	req := h.req("WRITE a.txt: first")
	req.SystemPrompt = workerCharter
	req.Tools = true
	req.OnSteerConsumed = func() { consumed <- struct{}{} }
	run, err := h.l.Start(context.Background(), "codex", req)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if !run.Steer("WRITE b.txt: second") {
		t.Fatal("the words must be kept for the next turn")
	}
	res := run.Wait()
	if !res.OK || len(consumed) != 1 {
		t.Fatalf("%+v (consumed %d)", res, len(consumed))
	}
	if _, err := os.Stat(filepath.Join(h.cwd, "b.txt")); err != nil || strings.Contains(res.Text, "a.txt") {
		t.Fatalf("a second turn carried the words out: %v (%s)", err, res.Text)
	}
	if invs := h.invocations(); len(invs) != 1 {
		t.Fatalf("one server, one thread: %+v", invs)
	}
}

func TestCancelAndTimeout(t *testing.T) {
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			h := newHarness(t, "FAKEAGENT_DELAY_MS=8000")
			req := h.req("ping")
			req.SystemPrompt = "connectivity check"
			run, err := h.l.Start(context.Background(), agent, req)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
			start := time.Now()
			run.Cancel()
			res := run.Wait()
			if !res.Cancelled || res.OK || time.Since(start) > 5*time.Second {
				t.Fatalf("cancel: %+v after %v", res, time.Since(start))
			}
			req.Timeout = 300 * time.Millisecond
			res = agentcli.Call(context.Background(), h.l, agent, req)
			if res.OK || !strings.Contains(res.Error, "timed out") {
				t.Fatalf("timeout: %+v", res)
			}
		})
	}
}

func TestProviderFailuresSurface(t *testing.T) {
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			h := newHarness(t)
			res := agentcli.Call(context.Background(), h.l, agent, h.req("FAKE_FAIL please"))
			if res.OK || res.Error == "" {
				t.Fatalf("a provider failure must not read as success: %+v", res)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	h := newHarness(t)
	d := agentcli.Detect(context.Background(), "codex", "", h.l.Env)
	if !d.Available || !strings.Contains(d.Version, "codex fake") {
		t.Fatalf("%+v", d)
	}
	missing := agentcli.Detect(context.Background(), "codex", "/nonexistent/codex", h.l.Env)
	if missing.Available || missing.Error == "" {
		t.Fatalf("%+v", missing)
	}
}

func TestAnEscapedGrandchildCannotHangTheRun(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	// Answers, then leaves a detached child holding stdout open for a minute.
	body := `#!/bin/sh
read line
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"OK","session_id":"s1","queued_turn_count":0}'
(trap '' HUP TERM; sleep 60) &
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &agentcli.Launcher{Binary: func(string) (string, error) { return script, nil }, RuntimeDir: dir, Env: os.Environ()}
	start := time.Now()
	res := agentcli.Call(context.Background(), l, "claude", agentcli.Request{Prompt: "x", Cwd: dir, Timeout: 30 * time.Second})
	if !res.OK || res.Text != "OK" {
		t.Fatalf("%+v", res)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("the run must end when the CLI does, not when its leftovers do: %v", time.Since(start))
	}
}

// What each CLI can run comes from the CLI when it can say (codex, pi), and
// from a built-in list when it cannot (claude).
func TestModelCatalogs(t *testing.T) {
	dir := fakecli.Dir(t)
	env := os.Environ()
	codex := agentcli.ListModels(context.Background(), "codex", filepath.Join(dir, "codex"), env)
	if codex.Source != "cli" || len(codex.Models) != 2 || codex.Models[0].ID != "gpt-fake-1" ||
		strings.Join(codex.Models[0].Efforts, ",") != "low,medium,high,xhigh" || codex.Models[0].DefaultEffort != "medium" {
		t.Fatalf("codex: %+v", codex)
	}
	pi := agentcli.ListModels(context.Background(), "pi", filepath.Join(dir, "pi"), env)
	if pi.Source != "cli" || len(pi.Models) != 2 || pi.Models[1].ID != "fakeprov/fake-model-b" || !slices.Contains(pi.Efforts, "off") {
		t.Fatalf("pi: %+v", pi)
	}
	claude := agentcli.ListModels(context.Background(), "claude", "", env)
	if claude.Source != "builtin" || len(claude.Models) == 0 || !slices.Contains(claude.Efforts, "max") {
		t.Fatalf("claude: %+v", claude)
	}
	broken := agentcli.ListModels(context.Background(), "codex", filepath.Join(t.TempDir(), "missing"), env)
	if broken.Error == "" || len(broken.Efforts) == 0 {
		t.Fatalf("a CLI that cannot answer still offers its efforts, and says why: %+v", broken)
	}
}
