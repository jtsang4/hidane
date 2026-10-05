package agentcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/settings"
)

// pi in RPC mode: JSONL commands on stdin, responses and session events on
// stdout. The run ends at `agent_settled`; its session file is read with
// get_state so a Manager can continue it next turn. The guard is a tiny
// extension that hands every tool call to `hidane guard --format pi`.
type piRun struct {
	p       *proc
	req     Request
	started time.Time

	mu          sync.Mutex
	cancelled   bool
	timedOut    bool
	settled     bool
	rejected    string
	sessionFile string
	text        string
	failure     string
	toolCalls   int
	steering    bool
}

// PiGuardShim is the pi extension that delegates tool calls to the hidane
// guard. It fails closed: a guard that cannot answer refuses the call.
const PiGuardShim = `import { spawnSync } from "node:child_process";

export default function hidaneGuard(pi: any): void {
  pi.on("tool_call", async (event: any) => {
    const bin = process.env.HIDANE_GUARD_BIN;
    if (!bin) return { block: true, reason: "hidane guard: HIDANE_GUARD_BIN is not set" };
    const res = spawnSync(bin, ["guard", "--format", "pi"], {
      input: JSON.stringify({ tool_name: event.toolName, tool_input: event.input ?? {} }),
      encoding: "utf8",
      env: process.env,
      timeout: 30000,
    });
    if (res.status !== 0) {
      return { block: true, reason: "hidane guard failed: " + (res.error?.message ?? res.stderr ?? String(res.status)) };
    }
    try {
      const decision = JSON.parse(res.stdout || "{}");
      return decision.block ? { block: true, reason: decision.reason } : undefined;
    } catch {
      return { block: true, reason: "hidane guard: unreadable decision" };
    }
  });
}
`

// EnsureRuntimeFiles writes generated helper files.
func (l *Launcher) EnsureRuntimeFiles() error {
	if err := os.MkdirAll(l.RuntimeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.piShimPath(), []byte(PiGuardShim), 0o644)
}

func (l *Launcher) piShimPath() string { return filepath.Join(l.RuntimeDir, "pi-guard.ts") }

func piArgs(l *Launcher, req Request) ([]string, []string) {
	args := []string{"--mode", "rpc", "--no-extensions"}
	if req.SessionDir != "" {
		args = append(args, "--session-dir", req.SessionDir)
	}
	args = append(args, systemPromptArgs(req)...)
	if req.Tools {
		args = append(args, "-e", l.piShimPath())
	} else {
		args = append(args, "--no-tools", "--no-skills", "--no-context-files", "--no-prompt-templates")
	}
	if p := req.Provider; p != nil {
		args = append(args, "--provider", p.PiProvider)
		if p.APIKey != "" {
			args = append(args, "--api-key", p.APIKey)
		}
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "--thinking", req.Effort)
	}
	if req.ResumeID != "" {
		args = append(args, "--session", req.ResumeID)
	}
	env := WithoutVars(l.Env, parentSessionVars...)
	if req.Provider != nil {
		// hidane chose the endpoint: an inherited ANTHROPIC_BASE_URL must not
		// redirect it.
		env = WithoutPrefix(env, "ANTHROPIC_")
	}
	env = append(env, "PI_OFFLINE=1", "HIDANE_GUARD_BIN="+l.GuardCommand)
	env = append(env, req.Env...)
	return args, env
}

func startPi(ctx context.Context, l *Launcher, bin string, req Request) (Run, error) {
	if err := settings.ValidateModel(settings.Pi, req.Model, req.Provider); err != nil {
		return nil, err
	}
	if req.Tools && !fileExists(l.piShimPath()) {
		if err := l.EnsureRuntimeFiles(); err != nil {
			return nil, err
		}
	}
	args, env := piArgs(l, req)
	p, err := startProc(bin, args, req.Cwd, env, true)
	if err != nil {
		return nil, err
	}
	r := &piRun{p: p, req: req, started: time.Now()}
	p.readLines(r.onLine)
	prompt := map[string]any{"id": "prompt", "type": "prompt", "message": req.Prompt}
	if imgs := piImages(req.Images); len(imgs) > 0 {
		prompt["images"] = imgs
	}
	if err := p.writeJSON(prompt); err != nil {
		p.kill()
		return nil, err
	}
	p.supervise(ctx, req.Timeout, func() {
		r.mu.Lock()
		r.timedOut = true
		r.mu.Unlock()
	})
	return r, nil
}

func piImages(images []Image) []any {
	var out []any
	for _, im := range images {
		data, err := im.base64()
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"type": "image", "data": data, "mimeType": im.MimeType})
	}
	return out
}

type piMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
}

func (m piMessage) text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(m.Content, &parts)
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func (r *piRun) onLine(line []byte) {
	var m struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Success   bool            `json:"success"`
		Error     string          `json:"error"`
		Data      json.RawMessage `json:"data"`
		Message   piMessage       `json:"message"`
		ToolName  string          `json:"toolName"`
		Args      json.RawMessage `json:"args"`
		Input     json.RawMessage `json:"input"`
		IsError   bool            `json:"isError"`
		Steering  []any           `json:"steering"`
		Assistant struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if json.Unmarshal(line, &m) != nil {
		return
	}
	switch m.Type {
	case "response":
		switch m.ID {
		case "prompt":
			if !m.Success {
				r.mu.Lock()
				r.rejected = m.Error
				r.mu.Unlock()
				r.p.closeStdin()
			}
		case "state":
			var st struct {
				SessionFile string `json:"sessionFile"`
			}
			_ = json.Unmarshal(m.Data, &st)
			r.mu.Lock()
			r.sessionFile = st.SessionFile
			r.mu.Unlock()
			r.p.closeStdin()
		}
	case "message_update":
		if m.Assistant.Type == "text_delta" && r.req.OnText != nil {
			r.req.OnText(m.Assistant.Delta)
		}
	case "message_end":
		if m.Message.Role == "assistant" {
			r.mu.Lock()
			if t := m.Message.text(); t != "" {
				r.text = t
			}
			if m.Message.StopReason == "error" {
				r.failure = m.Message.ErrorMessage
				if r.failure == "" {
					r.failure = "model request failed"
				}
			} else {
				r.failure = ""
			}
			r.mu.Unlock()
		}
	case "tool_execution_start":
		input := m.Args
		if len(input) == 0 {
			input = m.Input
		}
		r.mu.Lock()
		r.toolCalls++
		r.mu.Unlock()
		if r.req.OnTool != nil {
			r.req.OnTool(ToolEvent{Phase: "start", Tool: m.ToolName, Detail: clip(string(input), 500)})
		}
	case "tool_execution_end":
		if r.req.OnTool != nil {
			r.req.OnTool(ToolEvent{Phase: "end", Tool: m.ToolName, IsError: m.IsError})
		}
	case "queue_update":
		r.mu.Lock()
		consumed := r.steering && m.Steering != nil && len(m.Steering) == 0
		if consumed {
			r.steering = false
		}
		r.mu.Unlock()
		if consumed && r.req.OnSteerConsumed != nil {
			r.req.OnSteerConsumed()
		}
	case "agent_settled":
		r.mu.Lock()
		r.settled = true
		r.mu.Unlock()
		if err := r.p.writeJSON(map[string]any{"id": "state", "type": "get_state"}); err != nil {
			r.p.closeStdin()
		}
	}
}

func (r *piRun) Steer(text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled || r.settled {
		return false
	}
	if err := r.p.writeJSON(map[string]any{"type": "steer", "message": text}); err != nil {
		return false
	}
	r.steering = true
	return true
}

func (r *piRun) Cancel() {
	r.mu.Lock()
	r.cancelled = true
	r.mu.Unlock()
	_ = r.p.writeJSON(map[string]any{"type": "abort"})
	go func() {
		select {
		case <-r.p.done:
		case <-time.After(2 * time.Second):
		}
		r.p.closeStdin()
		r.p.kill()
	}()
}

func (r *piRun) Wait() Result {
	<-r.p.done
	r.mu.Lock()
	defer r.mu.Unlock()
	res := Result{Text: r.text, SessionID: r.sessionFile, ToolCalls: r.toolCalls, DurationMs: time.Since(r.started).Milliseconds()}
	switch {
	case r.cancelled:
		res.Cancelled, res.Error = true, "cancelled"
	case r.timedOut:
		res.Error = "timed out after " + r.req.Timeout.String()
	case r.rejected != "":
		res.Error = "pi rejected the prompt: " + r.rejected
	case r.failure != "":
		res.Error = r.failure
	case r.settled:
		res.OK = true
	default:
		res.Error = r.p.exitError("pi exited before the run settled")
	}
	return res
}
