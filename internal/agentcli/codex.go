package agentcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Codex as `codex app-server`: JSON-RPC over stdio, one server and one thread
// per run. The person's words reach the running turn through turn/steer and,
// once no turn runs, start the next one on the same thread — the execution is
// one, the turns may be several.
//
// The guard is a PreToolUse hook passed with -c. app-server skips such a hook,
// without a word, unless the thread's own config sets bypass_hook_trust (on
// the command line that key does nothing): a tool call the guard never saw
// therefore stops the run.
type codexRun struct {
	l       *Launcher
	req     Request
	p       *proc
	started time.Time
	done    chan struct{}
	turnEnd chan struct{}

	mu        sync.Mutex
	nextID    int
	calls     map[int]chan rpcReply
	threadID  string
	turnID    string
	turnState string
	// ended: how each of the thread's turns ended, by turn id.
	ended map[string]string
	// queue holds words that found no turn to steer; steering, words sent into
	// the running turn that it has not taken in yet.
	queue    []string
	steering []codexSteer
	steerSeq int
	// hooked: tool call ids the guard was asked about.
	hooked map[string]bool
	// finishing is set, under mu, once no further turn will run: steering
	// accepted after that point would be acknowledged and then dropped.
	finishing bool
	cancelled bool
	timedOut  bool
	text      string
	failure   string
	lastError string
	toolCalls int
	result    Result
}

type codexSteer struct{ id, text string }

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcReply struct {
	result json.RawMessage
	err    *rpcError
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

// codexToolFeatures are the codex features that hand the model a tool or an
// MCP server of codex's own; a reasoning role runs with all of them off.
var codexToolFeatures = []string{"shell_tool", "unified_exec", "multi_agent", "goals", "apps", "plugins", "browser_use",
	"in_app_browser", "computer_use", "image_generation", "view_image", "sleep_tool", "tool_suggest", "skill_search"}

// codexProfile is the permission profile a worker runs under.
const codexProfile = "hidane"

func codexConfig(l *Launcher, req Request) (cfg []string, env []string) {
	cfg = []string{"approval_policy=" + tomlString("never")}
	if req.Tools {
		// The workspace is where a worker starts, not a fence: it may change
		// files anywhere, and fetch things. The sandbox still keeps it off what
		// it is told to leave alone — where the guard cannot follow, since
		// codex does not show the hook a command's workdir.
		fs := []string{tomlString(":root") + "=" + tomlString("write"), tomlString(req.Cwd) + "=" + tomlString("write")}
		// Named, so they stay writable inside a read-only path: a worktree's
		// git metadata, a checkout the person lent.
		for _, p := range req.WritableRoots {
			fs = append(fs, tomlString(p)+"="+tomlString("write"))
		}
		for _, p := range req.ReadOnly {
			fs = append(fs, tomlString(p)+"="+tomlString("read"))
		}
		for _, p := range req.Hidden {
			fs = append(fs, tomlString(p)+"="+tomlString("deny"))
		}
		cfg = append(cfg,
			"default_permissions="+tomlString(codexProfile),
			"permissions."+codexProfile+".filesystem={"+strings.Join(fs, ",")+"}",
			"permissions."+codexProfile+".network.enabled=true",
			`hooks.PreToolUse=[{matcher="*",hooks=[{type="command",command=`+tomlString(hookCommand(l.GuardCommand, "codex"))+`,timeout=30}]}]`)
		if req.SystemPrompt != "" {
			cfg = append(cfg, "developer_instructions="+tomlString(req.SystemPrompt))
		}
	} else {
		// A reasoning role's charter replaces the base instructions instead
		// (thread/start): told it was a coding agent with a shell, a codex
		// Manager tried to write the files itself.
		cfg = append(cfg, "sandbox_mode="+tomlString("read-only"))
		for _, f := range codexToolFeatures {
			cfg = append(cfg, "features."+f+"=false")
		}
		cfg = append(cfg, "web_search="+tomlString("disabled"))
	}
	if req.Model != "" {
		cfg = append(cfg, "model="+tomlString(req.Model))
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

func codexArgs(l *Launcher, req Request) ([]string, []string) {
	cfg, env := codexConfig(l, req)
	args := []string{"app-server"}
	for _, c := range cfg {
		args = append(args, "-c", c)
	}
	return args, env
}

func codexInput(text string, images []Image) []any {
	in := []any{map[string]any{"type": "text", "text": text}}
	for _, im := range images {
		in = append(in, map[string]any{"type": "localImage", "path": im.Path})
	}
	return in
}

func startCodex(ctx context.Context, l *Launcher, bin string, req Request) (Run, error) {
	args, env := codexArgs(l, req)
	p, err := startProc(bin, args, req.Cwd, env, true)
	if err != nil {
		return nil, err
	}
	r := &codexRun{l: l, req: req, p: p, started: time.Now(), done: make(chan struct{}), turnEnd: make(chan struct{}, 1),
		calls: map[int]chan rpcReply{}, hooked: map[string]bool{}, ended: map[string]string{}}
	p.readLines(r.onLine)
	p.supervise(ctx, req.Timeout, func() {
		r.mu.Lock()
		r.timedOut = true
		r.mu.Unlock()
	})
	go r.drive()
	return r, nil
}

var errCodexExited = errors.New("codex app-server exited")

// call sends one request and waits for its answer, the server's exit, or
// (when within > 0) that long.
func (r *codexRun) call(method string, params any, within time.Duration) (json.RawMessage, error) {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	reply := make(chan rpcReply, 1)
	r.calls[id] = reply
	r.mu.Unlock()
	forget := func() {
		r.mu.Lock()
		delete(r.calls, id)
		r.mu.Unlock()
	}
	if err := r.p.writeJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		forget()
		return nil, err
	}
	var timeout <-chan time.Time
	if within > 0 {
		t := time.NewTimer(within)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case rep := <-reply:
		if rep.err != nil {
			return nil, fmt.Errorf("%s: %s", method, rep.err.Message)
		}
		return rep.result, nil
	case <-r.p.done:
		forget()
		return nil, errCodexExited
	case <-timeout:
		forget()
		return nil, fmt.Errorf("%s: no answer within %s", method, within)
	}
}

func (r *codexRun) drive() {
	defer close(r.done)
	defer r.finish()
	if err := r.open(); err != nil {
		r.fail(err)
		return
	}
	input := codexInput(r.req.Prompt, r.req.Images)
	for {
		if err := r.runTurn(input); err != nil {
			r.fail(err)
			return
		}
		r.mu.Lock()
		next := ""
		if !r.cancelled && !r.timedOut && r.turnState == "completed" && r.failure == "" && len(r.queue) > 0 {
			next = strings.Join(r.queue, "\n\n")
			r.queue = nil
		}
		if next == "" {
			r.finishing = true
		}
		r.mu.Unlock()
		if next == "" {
			return
		}
		if r.req.OnSteerConsumed != nil {
			r.req.OnSteerConsumed()
		}
		input = codexInput("The person added, while you were working:\n"+next, nil)
	}
}

func (r *codexRun) open() error {
	if _, err := r.call("initialize", map[string]any{"clientInfo": map[string]any{"name": "hidane", "title": "Hidane", "version": "1"}}, 0); err != nil {
		return err
	}
	if err := r.p.writeJSON(map[string]any{"method": "initialized"}); err != nil {
		return err
	}
	method, params := "thread/start", map[string]any{"cwd": r.req.Cwd}
	if r.req.ResumeID != "" {
		method, params["threadId"] = "thread/resume", r.req.ResumeID
	}
	if r.req.Tools {
		params["config"] = map[string]any{"bypass_hook_trust": true}
	} else {
		if r.req.SystemPrompt != "" {
			params["baseInstructions"] = r.req.SystemPrompt
		}
		off, err := r.mcpServersOff()
		if err != nil {
			return err
		}
		params["config"] = off
	}
	res, err := r.call(method, params, 0)
	if err != nil {
		return err
	}
	var out struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(res, &out) != nil || out.Thread.ID == "" {
		return fmt.Errorf("%s returned no thread", method)
	}
	r.mu.Lock()
	r.threadID = out.Thread.ID
	r.mu.Unlock()
	return nil
}

// mcpServersOff turns off, for this thread, every MCP server the person's own
// codex config declares: their tools are tools all the same.
func (r *codexRun) mcpServersOff() (map[string]any, error) {
	res, err := r.call("config/read", map[string]any{"cwd": r.req.Cwd}, 0)
	if err != nil {
		return nil, err
	}
	var out struct {
		Config struct {
			MCPServers map[string]json.RawMessage `json:"mcp_servers"`
		} `json:"config"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("config/read: %w", err)
	}
	off := map[string]any{}
	for name := range out.Config.MCPServers {
		off["mcp_servers."+name+".enabled"] = false
	}
	return off, nil
}

// runTurn starts a turn and returns once it has ended (or the server has).
func (r *codexRun) runTurn(input []any) error {
	r.mu.Lock()
	thread := r.threadID
	r.mu.Unlock()
	res, err := r.call("turn/start", map[string]any{"threadId": thread, "input": input}, 0)
	if err != nil {
		return err
	}
	var out struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(res, &out) != nil || out.Turn.ID == "" {
		return errors.New("turn/start returned no turn")
	}
	r.mu.Lock()
	r.turnID, r.turnState = out.Turn.ID, ""
	_, ended := r.ended[out.Turn.ID]
	waiting := r.queue
	r.queue = nil
	r.mu.Unlock()
	if !ended {
		// Words that arrived while the turn was being started go into it.
		for _, text := range waiting {
			r.Steer(text)
		}
		select {
		case <-r.turnEnd:
		case <-r.p.done:
			return errCodexExited
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ended {
		r.queue = append(waiting, r.queue...)
	}
	r.turnState = r.ended[out.Turn.ID]
	r.turnID = ""
	// Steered in but never taken in: the turn ended first, so they open the next.
	for _, s := range r.steering {
		r.queue = append(r.queue, s.text)
	}
	r.steering = nil
	return nil
}

func (r *codexRun) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure == "" && !errors.Is(err, errCodexExited) {
		r.failure = err.Error()
	}
}

func (r *codexRun) finish() {
	r.p.closeStdin()
	select {
	case <-r.p.done:
	case <-time.After(3 * time.Second):
		r.p.kill()
		<-r.p.done
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishing = true
	res := Result{Text: r.text, SessionID: r.threadID, ToolCalls: r.toolCalls, DurationMs: time.Since(r.started).Milliseconds()}
	switch {
	case r.cancelled:
		res.Cancelled, res.Error = true, "cancelled"
	case r.timedOut:
		res.Error = "timed out after " + r.req.Timeout.String()
	case r.failure != "":
		res.Error = r.failure
	case r.turnState == "completed":
		res.OK = true
	case r.turnState != "":
		res.Error = "codex turn " + r.turnState
		if r.lastError != "" {
			res.Error += ": " + r.lastError
		}
	default:
		res.Error = r.p.exitError("codex exited without completing the turn")
		if r.lastError != "" {
			res.Error += "\n" + r.lastError
		}
	}
	if len(r.queue) > 0 {
		res.Error += "\nthe person's later input was not delivered: " + strings.Join(r.queue, " / ")
	}
	r.result = res
}

type codexItem struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	ClientID string          `json:"clientId"`
	Command  string          `json:"command"`
	Status   string          `json:"status"`
	ExitCode *int            `json:"exitCode"`
	Changes  json.RawMessage `json:"changes"`
	Tool     string          `json:"tool"`
	Server   string          `json:"server"`
	Query    string          `json:"query"`
}

func codexToolName(it codexItem) (string, string, bool) {
	switch it.Type {
	case "commandExecution":
		return "bash", it.Command, true
	case "fileChange":
		return "edit", string(it.Changes), true
	case "mcpToolCall":
		return it.Server + "." + it.Tool, "", true
	case "webSearch":
		return "web_search", it.Query, true
	}
	return "", "", false
}

// guardedItem: the tool items app-server is known to show the PreToolUse hook
// first, with the hook run named after the item's call id.
func guardedItem(kind string) bool { return kind == "commandExecution" || kind == "fileChange" }

func (r *codexRun) onLine(line []byte) {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if json.Unmarshal(line, &m) != nil {
		return
	}
	switch {
	case len(m.ID) > 0 && m.Method != "":
		// The server's own requests share the id space with ours, so they are
		// told apart first — and answered at once: one left waiting hangs the turn.
		r.answerServer(m.ID, m.Method)
	case len(m.ID) > 0:
		var id int
		if json.Unmarshal(m.ID, &id) != nil {
			return
		}
		r.mu.Lock()
		reply := r.calls[id]
		delete(r.calls, id)
		r.mu.Unlock()
		if reply != nil {
			reply <- rpcReply{result: m.Result, err: m.Error}
		}
	default:
		r.onNotification(m.Method, m.Params)
	}
}

func (r *codexRun) answerServer(id json.RawMessage, method string) {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		_ = r.p.writeJSON(map[string]any{"id": id, "result": map[string]any{"decision": "decline"}})
	default:
		_ = r.p.writeJSON(map[string]any{"id": id, "error": map[string]any{"code": -32601, "message": "hidane does not handle " + method}})
	}
}

func (r *codexRun) onNotification(method string, raw json.RawMessage) {
	var n struct {
		ThreadID string    `json:"threadId"`
		Delta    string    `json:"delta"`
		Item     codexItem `json:"item"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Run struct {
			ID        string `json:"id"`
			EventName string `json:"eventName"`
			Source    string `json:"source"`
		} `json:"run"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		WillRetry bool `json:"willRetry"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return
	}
	r.mu.Lock()
	root := n.ThreadID != "" && n.ThreadID == r.threadID
	r.mu.Unlock()
	switch method {
	case "item/agentMessage/delta":
		if root && r.req.OnText != nil {
			r.req.OnText(n.Delta)
		}
	case "hook/started":
		// Only the hook hidane passed on the command line counts.
		if n.Run.EventName == "preToolUse" && n.Run.Source == "sessionFlags" {
			call := n.Run.ID[strings.LastIndex(n.Run.ID, ":")+1:]
			r.mu.Lock()
			r.hooked[call] = true
			r.mu.Unlock()
		}
	case "item/started":
		it := n.Item
		if it.Type == "userMessage" {
			r.steerTakenIn(it.ClientID)
			return
		}
		name, detail, ok := codexToolName(it)
		if !ok {
			return
		}
		if r.req.Tools && guardedItem(it.Type) {
			r.mu.Lock()
			seen := r.hooked[it.ID]
			if !seen && r.failure == "" {
				r.failure = fmt.Sprintf("stopped: codex ran a %s call (%s) without asking the hidane guard first", name, clip(detail, 200))
			}
			r.mu.Unlock()
			if !seen {
				go r.p.kill()
				return
			}
		}
		r.mu.Lock()
		r.toolCalls++
		r.mu.Unlock()
		if r.req.OnTool != nil {
			r.req.OnTool(ToolEvent{Phase: "start", Tool: name, Detail: clip(detail, 500)})
		}
	case "item/completed":
		it := n.Item
		if it.Type == "agentMessage" {
			if root {
				r.mu.Lock()
				r.text = it.Text
				r.mu.Unlock()
			}
			return
		}
		if name, _, ok := codexToolName(it); ok && r.req.OnTool != nil {
			failed := it.Status == "failed" || it.Status == "declined" || (it.ExitCode != nil && *it.ExitCode != 0)
			r.req.OnTool(ToolEvent{Phase: "end", Tool: name, IsError: failed})
		}
	case "error":
		if root && !n.WillRetry && n.Error.Message != "" {
			r.mu.Lock()
			r.lastError = n.Error.Message
			r.mu.Unlock()
		}
	case "turn/completed":
		if !root {
			return
		}
		r.mu.Lock()
		// Kept by id: the notification may overtake turn/start's own answer.
		r.ended[n.Turn.ID] = n.Turn.Status
		if n.Turn.Status == "failed" && r.failure == "" {
			r.failure = r.lastError
			if n.Turn.Error != nil && n.Turn.Error.Message != "" {
				r.failure = n.Turn.Error.Message
			}
			if r.failure == "" {
				r.failure = "codex turn failed"
			}
		}
		current := n.Turn.ID == r.turnID
		r.mu.Unlock()
		if current {
			select {
			case r.turnEnd <- struct{}{}:
			default:
			}
		}
	}
}

func (r *codexRun) steerTakenIn(clientID string) {
	if clientID == "" {
		return
	}
	r.mu.Lock()
	found := false
	for i, s := range r.steering {
		if s.id == clientID {
			r.steering = append(r.steering[:i], r.steering[i+1:]...)
			found = true
			break
		}
	}
	r.mu.Unlock()
	if found && r.req.OnSteerConsumed != nil {
		r.req.OnSteerConsumed()
	}
}

func (r *codexRun) Steer(text string) bool {
	r.mu.Lock()
	if r.cancelled || r.finishing {
		r.mu.Unlock()
		return false
	}
	if r.turnID == "" {
		r.queue = append(r.queue, text)
		r.mu.Unlock()
		return true
	}
	r.steerSeq++
	s := codexSteer{id: fmt.Sprintf("hidane-steer-%d", r.steerSeq), text: text}
	r.steering = append(r.steering, s)
	thread, turn := r.threadID, r.turnID
	r.mu.Unlock()
	_, err := r.call("turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "clientUserMessageId": s.id,
		"input": codexInput(text, nil)}, 10*time.Second)
	if err == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, pending := range r.steering {
		if pending.id == s.id {
			r.steering = append(r.steering[:i], r.steering[i+1:]...)
			if r.cancelled || r.finishing {
				return false
			}
			// The turn ended under it: the words open the next turn instead.
			r.queue = append(r.queue, text)
			return true
		}
	}
	// Already moved to the queue when the turn ended.
	return true
}

func (r *codexRun) Cancel() {
	r.mu.Lock()
	r.cancelled = true
	thread, turn := r.threadID, r.turnID
	r.mu.Unlock()
	go func() {
		if turn != "" {
			_, _ = r.call("turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, 2*time.Second)
		}
		r.p.kill()
	}()
}

func (r *codexRun) Wait() Result {
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}
