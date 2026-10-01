package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/settings"
)

// Codex in `exec --json` mode. A turn reads its prompt from stdin and exits;
// steering is honoured by continuing the same thread with `exec resume` once
// the current turn ends — the execution is one, the turns are several. The
// guard is a PreToolUse hook passed inline with -c.
type codexRun struct {
	l       *Launcher
	ctx     context.Context
	req     Request
	bin     string
	started time.Time
	done    chan struct{}

	mu        sync.Mutex
	cur       *proc
	queue     []string
	cancelled bool
	timedOut  bool
	threadID  string
	text      string
	failure   string
	turnDone  bool
	toolCalls int
	warnings  []string
	result    Result
}

// tomlString encodes a TOML basic string (JSON string syntax is a subset).
func tomlString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

const codexProviderID = "hidane"

func codexConfig(l *Launcher, req Request) (cfg []string, env []string) {
	sandbox := "read-only"
	if req.Tools {
		sandbox = "workspace-write"
	}
	cfg = []string{
		"sandbox_mode=" + tomlString(sandbox),
		"approval_policy=" + tomlString("never"),
	}
	if req.Tools {
		// Workers install packages and fetch things; the workspace stays the
		// only writable root.
		cfg = append(cfg, "sandbox_workspace_write.network_access=true",
			`hooks.PreToolUse=[{matcher="*",hooks=[{type="command",command=`+tomlString(hookCommand(l.GuardCommand, "codex"))+`,timeout=30}]}]`)
	}
	if req.SystemPrompt != "" {
		cfg = append(cfg, "developer_instructions="+tomlString(req.SystemPrompt))
	}
	if req.Effort != "" {
		cfg = append(cfg, "model_reasoning_effort="+tomlString(req.Effort))
	}
	env = WithoutVars(l.Env, parentSessionVars...)
	if p := req.Provider; p != nil {
		cfg = append(cfg,
			"model_provider="+tomlString(codexProviderID),
			"model_providers."+codexProviderID+".name="+tomlString(p.Label),
			"model_providers."+codexProviderID+".base_url="+tomlString(p.OpenAIBaseURL),
			"model_providers."+codexProviderID+".wire_api="+tomlString("responses"),
			"model_providers."+codexProviderID+".env_key="+tomlString("HIDANE_CODEX_API_KEY"),
			"model_providers."+codexProviderID+".requires_openai_auth=false",
		)
		env = append(env, "HIDANE_CODEX_API_KEY="+p.APIKey)
	}
	env = append(env, req.Env...)
	return cfg, env
}

func codexArgs(l *Launcher, req Request, resumeID string, withImages bool) ([]string, []string) {
	cfg, env := codexConfig(l, req)
	args := []string{"exec", "--json", "--color", "never"}
	tail := []string{"--skip-git-repo-check"}
	if req.Tools {
		tail = append(tail, "--dangerously-bypass-hook-trust")
	}
	for _, c := range cfg {
		tail = append(tail, "-c", c)
	}
	if req.Model != "" {
		tail = append(tail, "-m", req.Model)
	}
	if withImages {
		for _, im := range req.Images {
			tail = append(tail, "-i", im.Path)
		}
	}
	if resumeID != "" {
		args = append(args, "resume")
		args = append(args, tail...)
		args = append(args, resumeID, "-")
	} else {
		args = append(args, "-C", req.Cwd)
		args = append(args, tail...)
		args = append(args, "-")
	}
	return args, env
}

func startCodex(ctx context.Context, l *Launcher, bin string, req Request) (Run, error) {
	r := &codexRun{l: l, ctx: ctx, req: req, bin: bin, started: time.Now(), done: make(chan struct{}), threadID: req.ResumeID}
	if err := r.turn(req.Prompt, req.ResumeID, true); err != nil {
		return nil, err
	}
	go r.loop()
	return r, nil
}

func (r *codexRun) turn(prompt, resumeID string, first bool) error {
	args, env := codexArgs(r.l, r.req, resumeID, first)
	p, err := startProc(r.bin, args, r.req.Cwd, env, true)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.cur = p
	r.turnDone = false
	r.mu.Unlock()
	p.readLines(r.onLine)
	if err := p.writeRaw(prompt); err != nil {
		p.kill()
		return err
	}
	p.closeStdin()
	return nil
}

func (r *codexRun) loop() {
	defer close(r.done)
	deadline := time.NewTimer(r.req.Timeout)
	defer deadline.Stop()
	for {
		r.mu.Lock()
		p := r.cur
		r.mu.Unlock()
		select {
		case <-p.done:
		case <-r.ctx.Done():
			p.kill()
		case <-deadline.C:
			r.mu.Lock()
			r.timedOut = true
			r.mu.Unlock()
			p.kill()
		}
		<-p.done
		r.mu.Lock()
		next := ""
		if !r.cancelled && !r.timedOut && r.turnDone && r.failure == "" && len(r.queue) > 0 && r.threadID != "" {
			next = strings.Join(r.queue, "\n\n")
			r.queue = nil
		}
		thread := r.threadID
		r.mu.Unlock()
		if next == "" {
			r.finish(p)
			return
		}
		if r.req.OnSteerConsumed != nil {
			r.req.OnSteerConsumed()
		}
		if err := r.turn("The person added, while you were working:\n"+next, thread, false); err != nil {
			r.mu.Lock()
			r.failure = err.Error()
			r.mu.Unlock()
			r.finish(p)
			return
		}
	}
}

func (r *codexRun) finish(last *proc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res := Result{Text: r.text, SessionID: r.threadID, ToolCalls: r.toolCalls, DurationMs: time.Since(r.started).Milliseconds()}
	switch {
	case r.cancelled:
		res.Cancelled, res.Error = true, "cancelled"
	case r.timedOut:
		res.Error = "timed out after " + r.req.Timeout.String()
	case r.failure != "":
		res.Error = r.failure
	case r.turnDone:
		res.OK = true
	default:
		res.Error = last.exitError("codex exited without completing the turn")
		if len(r.warnings) > 0 {
			res.Error += "\n" + strings.Join(r.warnings, "\n")
		}
	}
	r.result = res
}

type codexItem struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Message  string          `json:"message"`
	Command  string          `json:"command"`
	Status   string          `json:"status"`
	ExitCode *int            `json:"exit_code"`
	Changes  json.RawMessage `json:"changes"`
	Tool     string          `json:"tool"`
	Server   string          `json:"server"`
	Query    string          `json:"query"`
}

func codexToolName(it codexItem) (string, string, bool) {
	switch it.Type {
	case "command_execution":
		return "bash", it.Command, true
	case "file_change":
		return "edit", string(it.Changes), true
	case "mcp_tool_call":
		return it.Server + "." + it.Tool, "", true
	case "web_search":
		return "web_search", it.Query, true
	}
	return "", "", false
}

func (r *codexRun) onLine(line []byte) {
	var m struct {
		Type     string    `json:"type"`
		ThreadID string    `json:"thread_id"`
		Item     codexItem `json:"item"`
		Message  string    `json:"message"`
		Error    struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &m) != nil {
		return
	}
	switch m.Type {
	case "thread.started":
		r.mu.Lock()
		if m.ThreadID != "" {
			r.threadID = m.ThreadID
		}
		r.mu.Unlock()
	case "item.started":
		if name, detail, ok := codexToolName(m.Item); ok {
			r.mu.Lock()
			r.toolCalls++
			r.mu.Unlock()
			if r.req.OnTool != nil {
				r.req.OnTool(ToolEvent{Phase: "start", Tool: name, Detail: clip(detail, 500)})
			}
		}
	case "item.completed":
		switch m.Item.Type {
		case "agent_message":
			r.mu.Lock()
			r.text = m.Item.Text
			r.mu.Unlock()
			if r.req.OnText != nil {
				r.req.OnText(m.Item.Text)
			}
		case "error":
			r.mu.Lock()
			r.warnings = append(r.warnings, m.Item.Message)
			r.mu.Unlock()
		default:
			if name, _, ok := codexToolName(m.Item); ok && r.req.OnTool != nil {
				failed := m.Item.Status == "failed" || (m.Item.ExitCode != nil && *m.Item.ExitCode != 0)
				r.req.OnTool(ToolEvent{Phase: "end", Tool: name, IsError: failed})
			}
		}
	case "turn.completed":
		r.mu.Lock()
		r.turnDone = true
		r.mu.Unlock()
	case "turn.failed":
		r.mu.Lock()
		r.failure = m.Error.Message
		if r.failure == "" {
			r.failure = "codex turn failed"
		}
		r.mu.Unlock()
	case "error":
		r.mu.Lock()
		if m.Message != "" {
			r.failure = m.Message
		}
		r.mu.Unlock()
	}
}

func (r *codexRun) Steer(text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
	}
	r.queue = append(r.queue, text)
	return true
}

func (r *codexRun) Cancel() {
	r.mu.Lock()
	r.cancelled = true
	p := r.cur
	r.mu.Unlock()
	if p != nil {
		go p.kill()
	}
}

func (r *codexRun) Wait() Result {
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

var _ = settings.Codex
