// Command fakeagent impersonates the claude, codex and pi CLIs for tests. It
// speaks each CLI's real wire protocol (as observed from the real binaries)
// and calls the guard hook it was configured with before every tool call, so
// tests exercise hidane's drivers and guard end to end without a model.
//
// The kind comes from FAKEAGENT_KIND or the executable name (claude, codex, pi).
// FAKEAGENT_DELAY_MS slows each turn down; FAKEAGENT_LOG appends a JSON record
// of every invocation (args, selected env) for assertions.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	kind := os.Getenv("FAKEAGENT_KIND")
	if kind == "" {
		kind = filepath.Base(os.Args[0])
	}
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Printf("%s fake 1.0.0\n", kind)
		return
	}
	logInvocation(kind, args)
	switch kind {
	case "claude":
		runClaude(args)
	case "codex":
		runCodex(args)
	case "pi":
		runPi(args)
	default:
		fmt.Fprintf(os.Stderr, "fakeagent: unknown kind %q\n", kind)
		os.Exit(2)
	}
}

func logInvocation(kind string, args []string) {
	path := os.Getenv("FAKEAGENT_LOG")
	if path == "" {
		return
	}
	env := map[string]string{}
	for _, name := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL", "HIDANE_CODEX_API_KEY",
		"HIDANE_POLICY_FILES", "HIDANE_PENDING_INPUT_FILE", "HIDANE_GUARD_BIN", "PI_OFFLINE", "CLAUDECODE"} {
		if v, ok := os.LookupEnv(name); ok {
			env[name] = v
		}
	}
	cwd, _ := os.Getwd()
	b, _ := json.Marshal(map[string]any{"kind": kind, "args": args, "env": env, "cwd": cwd})
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func flag(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"="), true
		}
	}
	return "", false
}

func has(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func delay() {
	if ms, err := strconv.Atoi(os.Getenv("FAKEAGENT_DELAY_MS")); err == nil && ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

var outMu sync.Mutex

func emit(v any) {
	b, _ := json.Marshal(v)
	outMu.Lock()
	defer outMu.Unlock()
	os.Stdout.Write(append(b, '\n'))
}

// ---- the scripted brain ----------------------------------------------------

var msgLine = regexp.MustCompile(`(?m)^\[(ev_[0-9a-z]+)\] \(([a-z]+)[^)]*\)\s?(.*)$`)

func effects(list ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"effects": list})
	return string(b)
}

func section(prompt, header string) string {
	i := strings.Index(prompt, header)
	if i < 0 {
		return ""
	}
	return prompt[i+len(header):]
}

func firstRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// brain answers like the role the system prompt describes.
func brain(system, prompt string) string {
	switch {
	case strings.Contains(system, "connectivity check") || strings.Contains(prompt, "connectivity check"):
		return "OK"
	case strings.Contains(system, "Primary agent of hidane"):
		if strings.Contains(prompt, "JUNK_PRIMARY") && !strings.Contains(prompt, "was not the JSON effect list") {
			return "<reasoning_effort>5</reasoning_effort>"
		}
		var list []map[string]any
		for _, m := range msgLine.FindAllStringSubmatch(section(prompt, "Messages this turn:"), -1) {
			id, kind, text := m[1], m[2], m[3]
			lower := strings.ToLower(text)
			switch {
			case kind == "external":
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "收到外部事件：" + firstRunes(text, 60)})
			case strings.Contains(lower, "hello") || strings.Contains(text, "你好"):
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "你好！我是 hidane 的主代理。"})
			case kind == "recall":
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "查到了之前的记录。"})
			case strings.Contains(text, "全部关闭"):
				// A model announces the change before it is made.
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "好的，这就全部关闭。"},
					map[string]any{"type": "set_status", "of": id, "all_open": true, "status": "closed"})
			default:
				list = append(list, map[string]any{"type": "create_work_item", "of": id, "title": firstRunes(text, 30),
					"brief": text, "repo": nil, "dispatch": true})
			}
		}
		return "```json\n" + effects(list...) + "\n```"
	case strings.Contains(system, "Manager of one work item"):
		turn := section(prompt, "Messages this turn:")
		if strings.Contains(turn, "(worker result") {
			if late := section(turn, "the worker never saw it:\n- "); late != "" {
				return effects(map[string]any{"type": "reply", "reply": "执行结束后才收到：" + firstRunes(late, 200)})
			}
			if strings.Contains(turn, ": blocked)") {
				return effects(map[string]any{"type": "escalate", "question": firstRunes(section(turn, "blocked on: "), 200), "tried": "ran a worker"})
			}
			if strings.Contains(turn, ": ok)") && strings.Contains(prompt, "AGAIN") && !strings.Contains(turn, "again.txt") {
				return effects(
					map[string]any{"type": "spawn", "instructions": "WRITE again.txt: second round", "expect": "again.txt"},
					map[string]any{"type": "reply", "reply": "已派出第二个 worker"},
				)
			}
			if strings.Contains(turn, ": ok)") {
				summary := section(turn, "summary:\n")
				reply := map[string]any{"type": "reply", "reply": "已完成：" + firstRunes(summary, 200)}
				if strings.Contains(prompt, "You are a CHILD work item") {
					// done before reply: the order a model may well choose.
					return effects(map[string]any{"type": "done"}, reply)
				}
				return effects(reply)
			}
			if strings.Contains(turn, ": cancelled)") {
				return effects(map[string]any{"type": "reply", "reply": "执行已取消。"})
			}
			return effects(map[string]any{"type": "reply", "reply": "执行失败：" + firstRunes(section(turn, "error: "), 200)})
		}
		if strings.Contains(prompt, "JUNK_ONCE") && !strings.Contains(prompt, "Your answer contained no action") {
			return "response."
		}
		if os.Getenv("FAKEAGENT_MANAGER_PLAIN") == "1" {
			return "我直接回答：这是纯文本。"
		}
		if strings.Contains(turn, "LONG_REPLY") {
			return effects(map[string]any{"type": "reply", "reply": "开头" + strings.Repeat("长", 11000) + "结尾"})
		}
		if strings.Contains(prompt, "ONLY_UNDERSTAND") && !strings.Contains(prompt, "Your answer contained no action") {
			// A model that restates the task and forgets to act.
			return effects(map[string]any{"type": "understanding", "text": "目标：先理解一下"})
		}
		if strings.Contains(turn, "(all child work items finished)") {
			return effects(map[string]any{"type": "reply", "reply": "子任务都完成了。"})
		}
		m := msgLine.FindStringSubmatch(turn)
		text := ""
		if m != nil {
			text = m[3]
		}
		return effects(
			map[string]any{"type": "understanding", "text": "目标：" + firstRunes(text, 80)},
			map[string]any{"type": "spawn", "instructions": workerInstructions(text), "expect": "result.txt exists"},
		)
	case strings.Contains(system, "memory distiller"):
		if m := regexp.MustCompile(`\[user\.message (wi_\w+)\] 任务约定：(\S+)`).FindStringSubmatch(prompt); m != nil {
			// A model words a memory afresh every time; only the existing list
			// keeps it from promoting the same thing again.
			existing, _, _ := strings.Cut(section(prompt, "Existing memories"), "Recent events:")
			if strings.Contains(existing, m[2]) {
				return `{"memories":[]}`
			}
			content := fmt.Sprintf("%s（进程 %d 记下）", m[2], os.Getpid())
			return fmt.Sprintf(`{"memories":[{"kind":"decision","scope":"work_item","work_item_id":%q,"content":%q,"confidence":0.9}]}`, m[1], content)
		}
		if strings.Contains(prompt, "记住") {
			return `{"memories":[{"kind":"preference","scope":"global","work_item_id":null,"content":"偏好简洁的回答","confidence":0.9}]}`
		}
		return `{"memories":[]}`
	}
	return "PONG"
}

// workerInstructions turns a person's request into fake-worker commands.
func workerInstructions(text string) string {
	if strings.Contains(text, "RUN:") {
		return text
	}
	return "WRITE result.txt: " + text
}

var (
	writeCmd = regexp.MustCompile(`WRITE (\S+): ?(.*)`)
	runCmd   = regexp.MustCompile(`RUN: (.+)`)
)

type toolCall struct {
	tool    string // claude/pi naming
	input   map[string]any
	perform func() error
}

// workerPlan derives the tool calls a worker would make.
func workerPlan(prompt, cwd string) []toolCall {
	var calls []toolCall
	if m := runCmd.FindStringSubmatch(prompt); m != nil {
		cmd := strings.TrimSpace(m[1])
		calls = append(calls, toolCall{tool: "Bash", input: map[string]any{"command": cmd}, perform: func() error {
			c := exec.Command("sh", "-c", cmd)
			c.Dir = cwd
			return c.Run()
		}})
	}
	if m := writeCmd.FindStringSubmatch(prompt); m != nil {
		path := filepath.Join(cwd, m[1])
		content := strings.TrimSpace(m[2])
		calls = append(calls, toolCall{tool: "Write", input: map[string]any{"file_path": path, "content": content}, perform: func() error {
			return os.WriteFile(path, []byte(content+"\n"), 0o644)
		}})
	}
	return calls
}

func workerSummary(prompt string, blocked []string, done []string) string {
	if strings.Contains(prompt, "NEEDS_INPUT") {
		return "I need a decision before continuing.\nBLOCKED: Which server should I deploy to?"
	}
	var b strings.Builder
	for _, d := range done {
		b.WriteString(d + "\n")
	}
	for _, r := range blocked {
		b.WriteString("Refused: " + r + "\n")
	}
	if b.Len() == 0 {
		b.WriteString("Nothing to do.")
	}
	return strings.TrimSpace(b.String())
}

// runHook invokes a hook command the way the real CLIs do: through a shell,
// the hook input as JSON on stdin. Returns a refusal reason, or "".
func runHook(command, format string, toolName string, input map[string]any) string {
	if command == "" {
		return ""
	}
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": toolName, "tool_input": input})
	c := exec.Command("sh", "-c", command)
	c.Stdin = strings.NewReader(string(payload))
	var out, errOut strings.Builder
	c.Stdout, c.Stderr = &out, &errOut
	err := c.Run()
	switch format {
	case "claude":
		if strings.Contains(out.String(), `"deny"`) {
			var d struct {
				H struct {
					Reason string `json:"permissionDecisionReason"`
				} `json:"hookSpecificOutput"`
			}
			_ = json.Unmarshal([]byte(out.String()), &d)
			return d.H.Reason
		}
	case "codex":
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
			return strings.TrimSpace(errOut.String())
		}
	case "pi":
		var d struct {
			Block  bool   `json:"block"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal([]byte(out.String()), &d) == nil && d.Block {
			return d.Reason
		}
	}
	if err != nil && format != "codex" {
		return "hook failed: " + err.Error()
	}
	return ""
}

// ---- claude -----------------------------------------------------------------

func systemPrompt(args []string) string {
	if s, ok := flag(args, "--system-prompt"); ok {
		return s
	}
	s, _ := flag(args, "--append-system-prompt")
	return s
}

func runClaude(args []string) {
	system := systemPrompt(args)
	session := "11111111-2222-3333-4444-" + fmt.Sprintf("%012d", time.Now().UnixNano()%1e12)
	if r, ok := flag(args, "--resume"); ok {
		session = r
	}
	hook := ""
	if s, ok := flag(args, "--settings"); ok {
		var doc struct {
			Hooks struct {
				Pre []struct {
					Hooks []struct {
						Command string `json:"command"`
					} `json:"hooks"`
				} `json:"PreToolUse"`
			} `json:"hooks"`
		}
		if json.Unmarshal([]byte(s), &doc) == nil && len(doc.Hooks.Pre) > 0 && len(doc.Hooks.Pre[0].Hooks) > 0 {
			hook = doc.Hooks.Pre[0].Hooks[0].Command
		}
	}
	tools, _ := flag(args, "--tools")
	toolsOn := !(has(args, "--tools") && tools == "")
	cwd, _ := os.Getwd()
	emit(map[string]any{"type": "system", "subtype": "init", "session_id": session, "cwd": cwd})

	queue := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var m struct {
				Message struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			text := ""
			for _, c := range m.Message.Content {
				if c.Type == "text" {
					text += c.Text
				}
			}
			queue <- text
		}
		close(queue)
	}()
	// FAKEAGENT_CLAUDE_ABSORB mimics real claude: a message that arrives while
	// a turn runs joins that turn (replayed, no result of its own).
	absorb := os.Getenv("FAKEAGENT_CLAUDE_ABSORB") == "1"
	n := 0
	for prompt := range queue {
		n++
		emit(map[string]any{"type": "user", "isReplay": true, "session_id": session,
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": prompt}}}})
		parts := []string{prompt}
		if absorb {
			delay()
		drain:
			for {
				select {
				case more, ok := <-queue:
					if !ok {
						break drain
					}
					emit(map[string]any{"type": "user", "isReplay": true, "session_id": session,
						"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": more}}}})
					parts = append(parts, more)
					prompt += "\n" + more
				default:
					break drain
				}
			}
		}
		var answer string
		if toolsOn && strings.Contains(system, "Worker execution") {
			var blocked, done []string
			var plan []toolCall
			for _, part := range parts {
				plan = append(plan, workerPlan(part, cwd)...)
			}
			for i, call := range plan {
				id := fmt.Sprintf("toolu_%d_%d", n, i)
				emit(map[string]any{"type": "assistant", "session_id": session, "message": map[string]any{"role": "assistant",
					"content": []any{map[string]any{"type": "tool_use", "id": id, "name": call.tool, "input": call.input}}}})
				reason := runHook(hook, "claude", call.tool, call.input)
				isErr := reason != ""
				if isErr {
					blocked = append(blocked, reason)
				} else if err := call.perform(); err != nil {
					isErr = true
					blocked = append(blocked, err.Error())
				} else {
					done = append(done, fmt.Sprintf("%s ok: %v", call.tool, call.input["file_path"]))
				}
				emit(map[string]any{"type": "user", "session_id": session, "message": map[string]any{"role": "user",
					"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isErr, "content": reason}}}})
			}
			delay()
			answer = workerSummary(prompt, blocked, done)
		} else {
			delay()
			answer = brain(system, prompt)
		}
		for _, chunk := range chunks(answer, 7) {
			emit(map[string]any{"type": "stream_event", "session_id": session,
				"event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": chunk}}})
		}
		emit(map[string]any{"type": "assistant", "session_id": session, "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": answer}}}})
		emit(map[string]any{"type": "result", "subtype": "success", "is_error": strings.Contains(prompt, "FAKE_FAIL"),
			"result": answer, "session_id": session, "queued_turn_count": len(queue)})
	}
}

func chunks(s string, n int) []string {
	r := []rune(s)
	var out []string
	for len(r) > 0 {
		k := n
		if k > len(r) {
			k = len(r)
		}
		out = append(out, string(r[:k]))
		r = r[k:]
	}
	return out
}

// ---- codex ------------------------------------------------------------------

var tomlCommand = regexp.MustCompile(`command="((?:[^"\\]|\\.)*)"`)

func runCodex(args []string) {
	if len(args) == 0 || args[0] != "exec" {
		fmt.Fprintln(os.Stderr, "fake codex: only exec is supported")
		os.Exit(2)
	}
	thread := fmt.Sprintf("0199%012d", time.Now().UnixNano()%1e12)
	for i, a := range args {
		if a == "resume" && i+1 < len(args) {
			rest := []string{}
			for j := i + 1; j < len(args); j++ {
				if args[j] == "-c" || args[j] == "-m" || args[j] == "-i" {
					j++
					continue
				}
				if !strings.HasPrefix(args[j], "-") {
					rest = append(rest, args[j])
				}
			}
			if len(rest) > 0 {
				thread = rest[0]
			}
		}
	}
	system, hook, sandbox := "", "", ""
	for i, a := range args {
		if a != "-c" || i+1 >= len(args) {
			continue
		}
		v := args[i+1]
		switch {
		case strings.HasPrefix(v, "developer_instructions="):
			_ = json.Unmarshal([]byte(strings.TrimPrefix(v, "developer_instructions=")), &system)
		case strings.HasPrefix(v, "hooks.PreToolUse="):
			if m := tomlCommand.FindStringSubmatch(v); m != nil {
				_ = json.Unmarshal([]byte(`"`+m[1]+`"`), &hook)
			}
		case strings.HasPrefix(v, "sandbox_mode="):
			_ = json.Unmarshal([]byte(strings.TrimPrefix(v, "sandbox_mode=")), &sandbox)
		}
	}
	cwd, _ := os.Getwd()
	if c, ok := flag(args, "-C"); ok {
		cwd = c
	}
	b, _ := readAll()
	prompt := string(b)
	emit(map[string]any{"type": "thread.started", "thread_id": thread})
	emit(map[string]any{"type": "turn.started"})
	var answer string
	if sandbox == "workspace-write" && strings.Contains(system, "Worker execution") {
		var blocked, done []string
		for i, call := range workerPlan(prompt, cwd) {
			id := fmt.Sprintf("item_%d", i)
			detail, _ := json.Marshal(call.input)
			emit(map[string]any{"type": "item.started", "item": map[string]any{"id": id, "type": "command_execution", "command": string(detail), "status": "in_progress"}})
			reason := runHook(hook, "codex", "Bash", map[string]any{"command": shellCommand(call)})
			exit := 0
			if reason != "" {
				blocked = append(blocked, reason)
				exit = 1
			} else if err := call.perform(); err != nil {
				blocked = append(blocked, err.Error())
				exit = 1
			} else {
				done = append(done, fmt.Sprintf("%s ok: %v", call.tool, call.input["file_path"]))
			}
			status := "completed"
			if exit != 0 {
				status = "failed"
			}
			emit(map[string]any{"type": "item.completed", "item": map[string]any{"id": id, "type": "command_execution", "command": string(detail), "exit_code": exit, "status": status}})
		}
		delay()
		answer = workerSummary(prompt, blocked, done)
	} else {
		delay()
		answer = brain(system, prompt)
	}
	emit(map[string]any{"type": "item.completed", "item": map[string]any{"id": "item_msg", "type": "agent_message", "text": answer}})
	if strings.Contains(prompt, "FAKE_FAIL") {
		emit(map[string]any{"type": "turn.failed", "error": map[string]any{"message": "fake failure"}})
		return
	}
	emit(map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}})
}

// shellCommand is how codex expresses a call: everything is a shell command.
func shellCommand(call toolCall) string {
	if cmd, ok := call.input["command"].(string); ok {
		return cmd
	}
	content, _ := call.input["content"].(string)
	path, _ := call.input["file_path"].(string)
	return fmt.Sprintf("printf '%%s\\n' '%s' > '%s'", strings.ReplaceAll(content, "'", `'\''`), path)
}

func readAll() ([]byte, error) {
	var out []byte
	buf := make([]byte, 64*1024)
	for {
		n, err := os.Stdin.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, nil
		}
	}
}

// ---- pi ---------------------------------------------------------------------

func runPi(args []string) {
	system := systemPrompt(args)
	toolsOn := !has(args, "--no-tools")
	sessionDir, _ := flag(args, "--session-dir")
	sessionFile, resumed := flag(args, "--session")
	if !resumed {
		sessionFile = filepath.Join(sessionDir, fmt.Sprintf("fake-%d.jsonl", time.Now().UnixNano()))
	}
	if sessionDir != "" {
		_ = os.MkdirAll(sessionDir, 0o755)
		_ = os.WriteFile(sessionFile, []byte("{}\n"), 0o644)
	}
	guardBin := os.Getenv("HIDANE_GUARD_BIN")
	hook := ""
	if toolsOn && has(args, "-e") && guardBin != "" {
		hook = "'" + guardBin + "' guard --format pi"
	}
	cwd, _ := os.Getwd()

	var mu sync.Mutex
	steers := []string{}
	aborted := false
	running := false
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var wg sync.WaitGroup
	for sc.Scan() {
		var cmd struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &cmd) != nil {
			emit(map[string]any{"type": "response", "command": "parse", "success": false, "error": "bad json"})
			continue
		}
		switch cmd.Type {
		case "prompt":
			emit(map[string]any{"id": cmd.ID, "type": "response", "command": "prompt", "success": true, "data": map[string]any{"disposition": "started"}})
			mu.Lock()
			running = true
			mu.Unlock()
			prompt := cmd.Message
			wg.Add(1)
			go func() {
				defer wg.Done()
				emit(map[string]any{"type": "agent_start"})
				var answer string
				if toolsOn && strings.Contains(system, "Worker execution") {
					var blocked, done []string
					for i, call := range workerPlan(prompt, cwd) {
						name := strings.ToLower(call.tool)
						emit(map[string]any{"type": "tool_execution_start", "toolCallId": fmt.Sprint(i), "toolName": name, "args": call.input})
						reason := runHook(hook, "pi", name, call.input)
						isErr := reason != ""
						if isErr {
							blocked = append(blocked, reason)
						} else if err := call.perform(); err != nil {
							isErr = true
							blocked = append(blocked, err.Error())
						} else {
							done = append(done, fmt.Sprintf("%s ok: %v", call.tool, call.input["file_path"]))
						}
						emit(map[string]any{"type": "tool_execution_end", "toolCallId": fmt.Sprint(i), "toolName": name, "isError": isErr})
					}
					delay()
					answer = workerSummary(prompt, blocked, done)
				} else {
					delay()
					answer = brain(system, prompt)
				}
				mu.Lock()
				wasAborted := aborted
				pending := steers
				steers = nil
				mu.Unlock()
				if len(pending) > 0 {
					emit(map[string]any{"type": "queue_update", "steering": []any{}})
					answer += "\n(also handled: " + strings.Join(pending, "; ") + ")"
				}
				stop := "stop"
				if wasAborted {
					stop = "aborted"
				}
				if strings.Contains(prompt, "FAKE_FAIL") {
					stop = "error"
				}
				for _, c := range chunks(answer, 7) {
					emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": c}})
				}
				msg := map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": answer}}, "stopReason": stop}
				if stop == "error" {
					msg["errorMessage"] = "fake provider failure"
				}
				emit(map[string]any{"type": "message_end", "message": msg})
				emit(map[string]any{"type": "agent_end"})
				mu.Lock()
				running = false
				mu.Unlock()
				emit(map[string]any{"type": "agent_settled"})
			}()
		case "steer":
			mu.Lock()
			steers = append(steers, cmd.Message)
			list := append([]string{}, steers...)
			mu.Unlock()
			emit(map[string]any{"id": cmd.ID, "type": "response", "command": "steer", "success": true, "data": map[string]any{"disposition": "queued"}})
			emit(map[string]any{"type": "queue_update", "steering": list})
		case "abort":
			mu.Lock()
			aborted = true
			mu.Unlock()
			emit(map[string]any{"id": cmd.ID, "type": "response", "command": "abort", "success": true})
		case "get_state":
			mu.Lock()
			r := running
			mu.Unlock()
			emit(map[string]any{"id": cmd.ID, "type": "response", "command": "get_state", "success": true,
				"data": map[string]any{"sessionFile": sessionFile, "sessionId": filepath.Base(sessionFile), "isStreaming": r}})
		}
	}
	wg.Wait()
}
