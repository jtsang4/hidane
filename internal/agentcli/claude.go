package agentcli

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/settings"
)

// Claude Code in print mode with stream-json on both sides: the prompt is the
// first user message on stdin, and stdin stays open so the person's later
// words can be queued as further user messages (steering). The guard is a
// PreToolUse hook injected through --settings.
type claudeRun struct {
	p       *proc
	req     Request
	started time.Time

	mu            sync.Mutex
	sent          int
	results       int
	replays       int
	cancelled     bool
	timedOut      bool
	sessionID     string
	resultSeen    bool
	resultText    string
	resultError   bool
	resultSubtype string
	assistantText string
	toolCalls     int
	tools         map[string]string
}

func hookCommand(guardBin, format string) string {
	return shellQuote(guardBin) + " guard --format " + format
}

func claudeArgs(l *Launcher, req Request) (args []string, env []string) {
	args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--include-partial-messages", "--replay-user-messages"}
	if req.SystemPrompt != "" {
		// A reasoning role is a router or planner, not a coding agent: told it
		// is Claude Code with file tools, a model claimed edits it never made.
		// The charter replaces the prompt there; workers keep it and append.
		flag := "--append-system-prompt"
		if !req.Tools {
			flag = "--system-prompt"
		}
		args = append(args, flag, req.SystemPrompt)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if e := effortFor(settings.Claude, req.Effort); e != "" {
		args = append(args, "--effort", e)
	}
	if req.ResumeID != "" {
		args = append(args, "--resume", req.ResumeID)
	}
	env = WithoutVars(l.Env, parentSessionVars...)
	doc := map[string]any{}
	if p := req.Provider; p != nil {
		// Mixing an inherited ANTHROPIC_* with the provider's would send one
		// endpoint's key to another.
		env = WithoutPrefix(env, "ANTHROPIC_")
		visible := map[string]string{"ANTHROPIC_BASE_URL": p.AnthropicBaseURL}
		if req.Model != "" {
			for _, name := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
				"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"} {
				visible[name] = req.Model
			}
		}
		for k, v := range visible {
			env = append(env, k+"="+v)
		}
		// The key goes through the environment only: command lines are
		// readable by every local user.
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+p.APIKey)
		doc["env"] = visible
		if req.Tools {
			// Server-side web search exists only on Anthropic's own API.
			args = append(args, "--disallowedTools", "WebSearch")
		}
	}
	if req.Tools {
		args = append(args, "--permission-mode", "bypassPermissions")
		doc["hooks"] = map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "*",
				"hooks":   []any{map[string]any{"type": "command", "command": hookCommand(l.GuardCommand, "claude"), "timeout": 30}},
			}},
		}
	} else {
		args = append(args, "--tools", "")
	}
	if len(doc) > 0 {
		b, _ := json.Marshal(doc)
		args = append(args, "--settings", string(b))
	}
	env = append(env, req.Env...)
	return args, env
}

func claudeUserMessage(text string, images []Image) map[string]any {
	content := []any{map[string]any{"type": "text", "text": text}}
	for _, im := range images {
		data, err := im.base64()
		if err != nil {
			continue
		}
		content = append(content, map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": im.MimeType, "data": data},
		})
	}
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}
}

func startClaude(ctx context.Context, l *Launcher, bin string, req Request) (Run, error) {
	args, env := claudeArgs(l, req)
	p, err := startProc(bin, args, req.Cwd, env, true)
	if err != nil {
		return nil, err
	}
	r := &claudeRun{p: p, req: req, started: time.Now(), tools: map[string]string{}}
	p.readLines(r.onLine)
	if err := p.writeJSON(claudeUserMessage(req.Prompt, req.Images)); err != nil {
		p.kill()
		return nil, err
	}
	r.sent = 1
	p.supervise(ctx, req.Timeout, func() {
		r.mu.Lock()
		r.timedOut = true
		r.mu.Unlock()
	})
	return r, nil
}

type claudeLine struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	SessionID   string          `json:"session_id"`
	Event       json.RawMessage `json:"event"`
	Message     json.RawMessage `json:"message"`
	Result      *string         `json:"result"`
	IsError     bool            `json:"is_error"`
	IsReplay    bool            `json:"isReplay"`
	QueuedTurns *int            `json:"queued_turn_count"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

func blocksOf(raw json.RawMessage) []claudeBlock {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var blocks []claudeBlock
	if json.Unmarshal(m.Content, &blocks) == nil {
		return blocks
	}
	return nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (r *claudeRun) onLine(line []byte) {
	var m claudeLine
	if json.Unmarshal(line, &m) != nil {
		return
	}
	switch m.Type {
	case "system":
		if m.Subtype == "init" && m.SessionID != "" {
			r.mu.Lock()
			r.sessionID = m.SessionID
			r.mu.Unlock()
		}
	case "stream_event":
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		// Thinking deltas are reasoning, not an answer; they never surface.
		if json.Unmarshal(m.Event, &ev) == nil && ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && r.req.OnText != nil {
			r.req.OnText(ev.Delta.Text)
		}
	case "assistant":
		var text strings.Builder
		for _, b := range blocksOf(m.Message) {
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				r.mu.Lock()
				r.toolCalls++
				r.tools[b.ID] = b.Name
				r.mu.Unlock()
				if r.req.OnTool != nil {
					r.req.OnTool(ToolEvent{Phase: "start", Tool: b.Name, Detail: clip(string(b.Input), 500)})
				}
			}
		}
		if text.Len() > 0 {
			r.mu.Lock()
			r.assistantText = text.String()
			r.mu.Unlock()
		}
	case "user":
		if m.IsReplay {
			r.mu.Lock()
			r.replays++
			steered := r.replays > 1
			r.mu.Unlock()
			if steered && r.req.OnSteerConsumed != nil {
				r.req.OnSteerConsumed()
			}
			return
		}
		for _, b := range blocksOf(m.Message) {
			if b.Type != "tool_result" {
				continue
			}
			r.mu.Lock()
			name := r.tools[b.ToolUseID]
			r.mu.Unlock()
			if r.req.OnTool != nil {
				r.req.OnTool(ToolEvent{Phase: "end", Tool: name, IsError: b.IsError})
			}
		}
	case "result":
		r.mu.Lock()
		r.results++
		r.resultSeen = true
		r.resultError = m.IsError
		r.resultSubtype = m.Subtype
		if m.Result != nil {
			r.resultText = *m.Result
		}
		if m.SessionID != "" {
			r.sessionID = m.SessionID
		}
		finished := r.results >= r.sent
		if m.QueuedTurns != nil {
			finished = *m.QueuedTurns == 0 && r.results >= r.sent
		}
		r.mu.Unlock()
		if finished {
			r.p.closeStdin()
		}
	}
}

func (r *claudeRun) Steer(text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled {
		return false
	}
	if err := r.p.writeJSON(claudeUserMessage(text, nil)); err != nil {
		return false
	}
	r.sent++
	return true
}

func (r *claudeRun) Cancel() {
	r.mu.Lock()
	r.cancelled = true
	r.mu.Unlock()
	r.p.closeStdin()
	go r.p.kill()
}

func (r *claudeRun) Wait() Result {
	<-r.p.done
	r.mu.Lock()
	defer r.mu.Unlock()
	res := Result{SessionID: r.sessionID, ToolCalls: r.toolCalls, DurationMs: time.Since(r.started).Milliseconds()}
	text := r.resultText
	if text == "" {
		text = r.assistantText
	}
	res.Text = text
	switch {
	case r.cancelled:
		res.Cancelled, res.Error = true, "cancelled"
	case r.timedOut:
		res.Error = "timed out after " + r.req.Timeout.String()
	case r.resultSeen && !r.resultError:
		res.OK = true
	case r.resultSeen:
		res.Error = strings.TrimSpace(r.resultText)
		if res.Error == "" {
			res.Error = "claude: " + r.resultSubtype
		}
	default:
		res.Error = r.p.exitError("claude exited without a result")
	}
	return res
}
