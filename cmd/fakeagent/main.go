// Command fakeagent impersonates the claude, codex and pi CLIs for tests. It
// speaks each CLI's real wire protocol (as observed from the real binaries)
// and calls the guard hook it was configured with before every tool call, so
// tests exercise hidane's drivers and guard end to end without a model.
//
// The kind is the executable's name (claude, codex, pi).
// FAKEAGENT_DELAY_MS slows each turn down, FAKEAGENT_WORKER_DELAY_MS only a
// worker's (catching a run mid-flight then costs no reasoning turns);
// FAKEAGENT_LOG appends a JSON record of every invocation (args, selected env)
// for assertions.
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

	"github.com/jtsang4/hidane/internal/agentcli/fakecli"
	"github.com/jtsang4/hidane/internal/clip"
)

func main() {
	kind := filepath.Base(os.Args[0])
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Printf("%s fake 1.0.0\n", kind)
		return
	}
	// What the real CLIs print when asked for their models.
	if kind == "codex" && len(args) >= 2 && args[0] == "debug" && args[1] == "models" {
		fmt.Println(`{"models":[` +
			`{"slug":"gpt-fake-1","display_name":"GPT Fake 1","visibility":"list","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}]},` +
			`{"slug":"gpt-fake-mini","display_name":"GPT Fake Mini","visibility":"list","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}]},` +
			`{"slug":"gpt-hidden","visibility":"hide","supported_reasoning_levels":[{"effort":"low"}]}]}`)
		return
	}
	if kind == "pi" && has(args, "--list-models") {
		fmt.Println("provider       model                context  max-out  thinking  images")
		fmt.Println("fakeprov       fake-model-a         128K     32K      yes       no")
		fmt.Println("fakeprov       fake-model-b         1M       64K      yes       yes")
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

func delay(worker bool) {
	ms, err := strconv.Atoi(os.Getenv("FAKEAGENT_DELAY_MS"))
	if v, e := strconv.Atoi(os.Getenv("FAKEAGENT_WORKER_DELAY_MS")); worker && e == nil {
		ms, err = v, e
	}
	if err == nil && ms > 0 {
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

var (
	msgLine      = regexp.MustCompile(fakecli.MessageLine)
	itemDecision = regexp.MustCompile(fakecli.DistilledItemMessage + `任务约定：(\S+)`)
)

var lookFirst = regexp.MustCompile(`LOOK_FIRST: ([^\n]+?)(?: END|$)`)

// Hints a test puts in a message to steer the fake Primary: REPO=<name or
// path> (several allowed), FROM=<work item>, INPLACE (UNASKED: without quoting
// the person), ROUTE=<work item>.
var (
	repoHint  = regexp.MustCompile(`REPO=(\S+)`)
	fromHint  = regexp.MustCompile(`FROM=(wi_\w+)`)
	routeHint = regexp.MustCompile(`ROUTE=(wi_\w+)`)
)

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

// brain answers like the role the system prompt describes.
func brain(system, prompt string) string {
	switch {
	case strings.Contains(system, fakecli.PingRole):
		return "OK"
	case strings.Contains(system, fakecli.PrimaryRole):
		if strings.Contains(prompt, "JUNK_PRIMARY") && !strings.Contains(prompt, fakecli.PrimaryRetry) {
			return "<reasoning_effort>5</reasoning_effort>"
		}
		if strings.Contains(section(prompt, fakecli.MessagesThisTurn), "PLAIN_PRIMARY") {
			// A model that answers in prose, asked twice.
			return "我直接回答，没有效果列表。"
		}
		var list []map[string]any
		demo := loadDemo()
		for _, m := range msgLine.FindAllStringSubmatch(section(prompt, fakecli.MessagesThisTurn), -1) {
			id, kind, text := m[1], m[2], m[3]
			lower := strings.ToLower(text)
			if effect, ok := demoPrimary(demo, id, text); ok && kind == "user" {
				list = append(list, effect)
				continue
			}
			switch {
			case kind == "external":
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "收到外部事件：" + clip.Runes(strings.TrimSpace(text), 60)})
			case strings.Contains(text, "代码示例"):
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "可以这样跑测试：\n\n```sh\nmake test\npnpm -C frontend test\n```"})
			case strings.Contains(text, "流程图"):
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "消息是这样流转的：\n\n```mermaid\nflowchart LR\n  A[收到消息] --> B{要动手吗}\n  B -->|是| C[派给 worker]\n  B -->|否| D[直接回复]\n```"})
			case strings.Contains(lower, "hello") || strings.Contains(text, "你好"):
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "你好！我是 hidane 的主代理。"})
			case kind == "recall":
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "查到了之前的记录。"})
			case strings.Contains(text, "停止并全部关闭"):
				// A model announces the change before it is made.
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "好的，这就停止并关闭。"},
					map[string]any{"type": "cancel", "of": id, "all_running": true},
					map[string]any{"type": "set_status", "of": id, "all_open": true, "status": "closed"})
			case strings.Contains(text, "关闭所有任务"):
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "好的，这就关闭。"},
					map[string]any{"type": "set_status", "of": id, "all_open": true, "status": "closed"})
			case strings.Contains(text, "停止正在跑的"):
				list = append(list, map[string]any{"type": "reply", "of": id, "reply": "好的，这就停止。"},
					map[string]any{"type": "cancel", "of": id, "all_running": true})
			case routeHint.MatchString(text):
				list = append(list, map[string]any{"type": "route", "of": id, "work_item_id": routeHint.FindStringSubmatch(text)[1],
					"message": text, "confidence": 1.0})
			default:
				effect := map[string]any{"type": "create_work_item", "of": id, "title": clip.Runes(strings.TrimSpace(text), 30),
					"brief": text, "repos": []any{}, "dispatch": true}
				var repos []any
				for _, m := range repoHint.FindAllStringSubmatch(text, -1) {
					spec := map[string]any{"repo": m[1], "in_place": strings.Contains(text, "INPLACE")}
					if strings.Contains(text, "INPLACE") && !strings.Contains(text, "UNASKED") {
						spec["asked"] = "INPLACE"
					}
					if f := fromHint.FindStringSubmatch(text); f != nil {
						spec["from"] = f[1]
					}
					repos = append(repos, spec)
				}
				if repos != nil {
					effect["repos"] = repos
				}
				list = append(list, effect)
			}
		}
		return "```json\n" + effects(list...) + "\n```"
	case strings.Contains(system, fakecli.ManagerRole):
		turn := section(prompt, fakecli.MessagesThisTurn)
		if answer, ok := demoManager(loadDemo(), prompt, turn); ok {
			return answer
		}
		if strings.Contains(turn, fakecli.WorkerResult) {
			if late := section(turn, fakecli.SteerLate); late != "" {
				return effects(map[string]any{"type": "reply", "reply": "执行结束后才收到：" + clip.Runes(strings.TrimSpace(late), 200)})
			}
			if given := section(turn, fakecli.SteerGiven); given != "" && strings.Contains(turn, fakecli.ResultOK) {
				return effects(map[string]any{"type": "reply", "reply": "已完成，含执行中的补充：" + clip.Runes(strings.TrimSpace(given), 200)})
			}
			if strings.Contains(turn, fakecli.ResultBlocked) {
				return effects(map[string]any{"type": "escalate", "question": clip.Runes(strings.TrimSpace(section(turn, fakecli.BlockedOn)), 200), "tried": "ran a worker"})
			}
			if strings.Contains(turn, fakecli.ResultOK) && strings.Contains(prompt, "AGAIN") && !strings.Contains(turn, "again.txt") {
				return effects(
					map[string]any{"type": "spawn", "instructions": "WRITE again.txt: second round", "expect": "again.txt"},
					map[string]any{"type": "reply", "reply": "已派出第二个 worker"},
				)
			}
			if strings.Contains(turn, fakecli.ResultOK) {
				summary := section(turn, fakecli.ResultSummary)
				reply := map[string]any{"type": "reply", "reply": "已完成：" + clip.Runes(strings.TrimSpace(summary), 200)}
				if strings.Contains(prompt, fakecli.ChildItem) {
					// done before reply: the order a model may well choose.
					return effects(map[string]any{"type": "done"}, reply)
				}
				return effects(reply)
			}
			if strings.Contains(turn, fakecli.ResultCancelled) {
				return effects(map[string]any{"type": "reply", "reply": "执行已取消。"})
			}
			return effects(map[string]any{"type": "reply", "reply": "执行失败：" + clip.Runes(strings.TrimSpace(section(turn, fakecli.ResultError)), 200)})
		}
		if strings.Contains(prompt, "JUNK_ONCE") && !strings.Contains(prompt, fakecli.ManagerNudge) {
			return "response."
		}
		if os.Getenv("FAKEAGENT_MANAGER_PLAIN") == "1" {
			return "我直接回答：这是纯文本。"
		}
		if strings.Contains(turn, "BAD_CHILDREN") {
			return effects(map[string]any{"type": "create_children", "children": []any{
				map[string]any{"title": "", "brief": "调研 A 的价格"},
				map[string]any{"title": "调研 B", "brief": "调研 B 的价格"},
			}}, map[string]any{"type": "reply", "reply": "已拆分"})
		}
		if strings.Contains(turn, "SPLIT_CHILDREN") {
			return effects(map[string]any{"type": "create_children", "children": []any{
				map[string]any{"title": "part A", "brief": "写 part A"},
				map[string]any{"title": "part B", "brief": "写 part B"},
			}}, map[string]any{"type": "reply", "reply": "已拆分成两个子任务"})
		}
		if strings.Contains(turn, "LONG_REPLY") {
			return effects(map[string]any{"type": "reply", "reply": "开头" + strings.Repeat("长", 11000) + "结尾"})
		}
		if strings.Contains(prompt, "ONLY_UNDERSTAND") && !strings.Contains(prompt, fakecli.ManagerNudge) {
			// A model that restates the task and forgets to act.
			return effects(map[string]any{"type": "understanding", "text": "目标：先理解一下"})
		}
		if strings.Contains(turn, fakecli.ChildrenSettled) {
			return effects(map[string]any{"type": "reply", "reply": "子任务都完成了。"})
		}
		if strings.Contains(turn, "ASK_OPTIONS") {
			return effects(map[string]any{"type": "escalate", "question": "出发时间选哪个？", "tried": "查过两天的航班",
				"options": []any{"周五晚上", "周六早上"}})
		}
		m := msgLine.FindStringSubmatch(turn)
		text := ""
		if m != nil {
			text = m[3]
		}
		return effects(
			map[string]any{"type": "understanding", "text": "目标：" + clip.Runes(strings.TrimSpace(text), 80)},
			map[string]any{"type": "spawn", "instructions": workerInstructions(text), "expect": "result.txt exists"},
		)
	case strings.Contains(system, fakecli.DistillerRole):
		if m := itemDecision.FindStringSubmatch(prompt); m != nil {
			// A model words a memory afresh every time; only the existing list
			// keeps it from promoting the same thing again.
			existing, _, _ := strings.Cut(section(prompt, fakecli.ExistingMemories), fakecli.RecentEvents)
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

// worker tracks what the guard and the file system made of a worker's calls.
type worker struct {
	blocked, done []string
	// report ends the summary, as a long account of the work would.
	report string
}

// LONG_SUMMARY=<n> makes a worker end its summary with n characters.
var longSummary = regexp.MustCompile(`LONG_SUMMARY=(\d+)`)

func newWorker(prompt string) worker {
	var w worker
	if m := longSummary.FindStringSubmatch(prompt); m != nil {
		n, _ := strconv.Atoi(m[1])
		w.report = strings.Repeat("长", n)
	}
	return w
}

// run performs a call the hook did not refuse; it reports whether the call failed.
func (w *worker) run(call toolCall, refused string) bool {
	if refused != "" {
		w.blocked = append(w.blocked, refused)
		return true
	}
	if err := call.perform(); err != nil {
		w.blocked = append(w.blocked, err.Error())
		return true
	}
	w.done = append(w.done, fmt.Sprintf("%s ok: %v", call.tool, call.input["file_path"]))
	return false
}

func (w *worker) summary() string {
	var b strings.Builder
	for _, d := range w.done {
		b.WriteString(d + "\n")
	}
	for _, r := range w.blocked {
		b.WriteString("Refused: " + r + "\n")
	}
	b.WriteString(w.report)
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
	// FAKEAGENT_CLAUDE_MERGE mimics it at a turn's end: the messages queued by
	// then make up the next turn together, as one user message.
	absorb := os.Getenv("FAKEAGENT_CLAUDE_ABSORB") == "1"
	merge := os.Getenv("FAKEAGENT_CLAUDE_MERGE") == "1"
	n := 0
	for prompt := range queue {
		n++
		parts := []string{prompt}
		if merge {
		waiting:
			for {
				select {
				case more, ok := <-queue:
					if !ok {
						break waiting
					}
					parts = append(parts, more)
				default:
					break waiting
				}
			}
			prompt = strings.Join(parts, "\n")
		}
		var content []any
		for _, part := range parts {
			content = append(content, map[string]any{"type": "text", "text": part})
		}
		emit(map[string]any{"type": "user", "isReplay": true, "session_id": session,
			"message": map[string]any{"role": "user", "content": content}})
		if absorb {
			delay(toolsOn && strings.Contains(system, fakecli.WorkerRole))
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
		if toolsOn && strings.Contains(system, fakecli.WorkerRole) {
			wk := newWorker(prompt)
			var plan []toolCall
			for _, part := range parts {
				plan = append(plan, workerPlan(part, cwd)...)
			}
			for i, call := range plan {
				id := fmt.Sprintf("toolu_%d_%d", n, i)
				emit(map[string]any{"type": "assistant", "session_id": session, "message": map[string]any{"role": "assistant",
					"content": []any{map[string]any{"type": "tool_use", "id": id, "name": call.tool, "input": call.input}}}})
				reason := runHook(hook, "claude", call.tool, call.input)
				isErr := wk.run(call, reason)
				emit(map[string]any{"type": "user", "session_id": session, "message": map[string]any{"role": "user",
					"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isErr, "content": reason}}}})
			}
			delay(true)
			answer = wk.summary()
		} else {
			// A reasoning role with tools looks first when told to: LOOK_FIRST: <command>.
			if m := lookFirst.FindStringSubmatch(prompt); toolsOn && m != nil {
				id := fmt.Sprintf("toolu_%d_look", n)
				input := map[string]any{"command": strings.TrimSpace(m[1])}
				emit(map[string]any{"type": "assistant", "session_id": session, "message": map[string]any{"role": "assistant",
					"content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": input}}}})
				reason := runHook(hook, "claude", "Bash", input)
				if reason == "" {
					c := exec.Command("sh", "-c", strings.TrimSpace(m[1]))
					c.Dir = cwd
					_ = c.Run()
				}
				emit(map[string]any{"type": "user", "session_id": session, "message": map[string]any{"role": "user",
					"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": reason != "", "content": reason}}}})
			}
			delay(false)
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

var (
	tomlCommand = regexp.MustCompile(`command="((?:[^"\\]|\\.)*)"`)
	tomlKey     = regexp.MustCompile(`[{,]"((?:[^"\\]|\\.)*)"=`)
)

// codexTurn is the running turn of the fake app-server.
type codexTurn struct {
	id, thread  string
	steers      []codexSteer
	interrupted bool
}

type codexSteer struct{ clientID, text string }

// runCodex is `codex app-server`: JSON-RPC over stdio. As in the real one, a
// PreToolUse hook given with -c runs only when the thread's config sets
// bypass_hook_trust, a call the hook refuses leaves no tool item behind, and
// steered input joins the running turn at its next step.
// FAKEAGENT_CODEX_SKIP_HOOKS=1 plays a codex that ignores the hook anyway;
// FAKEAGENT_CODEX_REFUSE_STEER=1 one whose turn is always just ending. Its
// config declares one MCP server of the person's own, fake_docs, and
// FAKEAGENT_THREAD_LOG appends the params of every thread/start|resume.
func runCodex(args []string) {
	if len(args) == 0 || args[0] != "app-server" {
		fmt.Fprintln(os.Stderr, "fake codex: only app-server is supported")
		os.Exit(2)
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
		case strings.HasPrefix(v, "permissions.") && strings.Contains(v, ".filesystem={"):
			// As the real one: a key twice makes the table unreadable TOML.
			seen := map[string]bool{}
			for _, m := range tomlKey.FindAllStringSubmatch(v, -1) {
				if seen[m[1]] {
					fmt.Fprintf(os.Stderr, "Error: invalid type: string %q, expected a map (duplicate key %s)\n", v, m[1])
					os.Exit(1)
				}
				seen[m[1]] = true
			}
		}
	}
	cwd, _ := os.Getwd()
	var (
		mu      sync.Mutex
		thread  string
		trusted bool
		turn    *codexTurn
		turns   int
		wg      sync.WaitGroup
	)
	reply := func(id json.RawMessage, result any) { emit(map[string]any{"id": id, "result": result}) }
	replyErr := func(id json.RawMessage, msg string) {
		emit(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": msg}})
	}
	notify := func(method string, params map[string]any) { emit(map[string]any{"method": method, "params": params}) }
	item := func(t *codexTurn, phase string, it map[string]any) {
		notify("item/"+phase, map[string]any{"threadId": t.thread, "turnId": t.id, "item": it})
	}
	// takeSteers hands over what was steered in, as a real turn takes it in
	// before its next model request.
	takeSteers := func(t *codexTurn) []string {
		mu.Lock()
		pending := t.steers
		t.steers = nil
		mu.Unlock()
		var texts []string
		for _, s := range pending {
			it := map[string]any{"type": "userMessage", "id": "um_" + s.clientID, "clientId": s.clientID,
				"content": []any{map[string]any{"type": "text", "text": s.text}}}
			item(t, "started", it)
			item(t, "completed", it)
			texts = append(texts, s.text)
		}
		return texts
	}
	runTurn := func(t *codexTurn, prompt string) {
		defer wg.Done()
		notify("turn/started", map[string]any{"threadId": t.thread, "turn": map[string]any{"id": t.id, "status": "inProgress", "items": []any{}}})
		um := map[string]any{"type": "userMessage", "id": "um_" + t.id, "content": []any{map[string]any{"type": "text", "text": prompt}}}
		item(t, "started", um)
		item(t, "completed", um)
		var answer string
		if sandbox != "read-only" && strings.Contains(system, fakecli.WorkerRole) {
			wk := newWorker(prompt)
			n := 0
			for inputs := []string{prompt}; len(inputs) > 0; {
				text := inputs[0]
				inputs = inputs[1:]
				for _, call := range workerPlan(text, cwd) {
					n++
					id := fmt.Sprintf("call_%s_%d", t.id, n)
					command := shellCommand(call)
					reason := ""
					mu.Lock()
					hooked := trusted && hook != "" && os.Getenv("FAKEAGENT_CODEX_SKIP_HOOKS") != "1"
					mu.Unlock()
					if hooked {
						run := map[string]any{"id": "pre-tool-use:0:/<session-flags>/config.toml:" + id, "eventName": "preToolUse",
							"source": "sessionFlags", "sourcePath": "/<session-flags>/config.toml", "status": "running", "entries": []any{}}
						notify("hook/started", map[string]any{"threadId": t.thread, "turnId": t.id, "run": run})
						reason = runHook(hook, "codex", "Bash", map[string]any{"command": command})
						run["status"], run["entries"] = "completed", []any{}
						if reason != "" {
							run["status"], run["entries"] = "blocked", []any{map[string]any{"kind": "feedback", "text": reason}}
						}
						notify("hook/completed", map[string]any{"threadId": t.thread, "turnId": t.id, "run": run})
					}
					if reason != "" {
						wk.run(call, reason)
						continue
					}
					ex := map[string]any{"type": "commandExecution", "id": id, "command": command, "cwd": cwd, "status": "inProgress"}
					item(t, "started", ex)
					ex["exitCode"], ex["status"] = 0, "completed"
					if wk.run(call, "") {
						ex["exitCode"], ex["status"] = 1, "failed"
					}
					item(t, "completed", ex)
				}
				delay(true)
				inputs = append(inputs, takeSteers(t)...)
			}
			answer = wk.summary()
		} else {
			delay(false)
			if more := takeSteers(t); len(more) > 0 {
				prompt += "\n" + strings.Join(more, "\n")
			}
			answer = brain(system, prompt)
		}
		mu.Lock()
		interrupted := t.interrupted
		mu.Unlock()
		status := "completed"
		var turnErr any
		switch {
		case interrupted:
			status = "interrupted"
		case strings.Contains(prompt, "FAKE_FAIL"):
			notify("error", map[string]any{"threadId": t.thread, "turnId": t.id, "willRetry": false, "error": map[string]any{"message": "fake failure"}})
			status, turnErr = "failed", map[string]any{"message": "fake failure"}
		default:
			msgID := "msg_" + t.id
			item(t, "started", map[string]any{"type": "agentMessage", "id": msgID, "text": ""})
			for _, c := range chunks(answer, 7) {
				notify("item/agentMessage/delta", map[string]any{"threadId": t.thread, "turnId": t.id, "itemId": msgID, "delta": c})
			}
			item(t, "completed", map[string]any{"type": "agentMessage", "id": msgID, "text": answer, "phase": "final_answer"})
		}
		mu.Lock()
		turn = nil
		mu.Unlock()
		notify("turn/completed", map[string]any{"threadId": t.thread, "turn": map[string]any{"id": t.id, "status": status, "items": []any{}, "error": turnErr}})
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Raw    json.RawMessage `json:"params"`
			Params struct {
				ThreadID         string `json:"threadId"`
				ExpectedTurnID   string `json:"expectedTurnId"`
				ClientID         string `json:"clientUserMessageId"`
				BaseInstructions string `json:"baseInstructions"`
				Config           struct {
					BypassHookTrust bool `json:"bypass_hook_trust"`
				} `json:"config"`
				Input []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"input"`
			} `json:"-"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.Method == "" {
			continue
		}
		_ = json.Unmarshal(req.Raw, &req.Params)
		text := ""
		for _, in := range req.Params.Input {
			if in.Type == "text" {
				text += in.Text
			}
		}
		switch req.Method {
		case "initialize":
			reply(req.ID, map[string]any{"userAgent": "codex fake", "platformFamily": "unix", "platformOs": "macos"})
		case "initialized":
		case "config/read":
			reply(req.ID, map[string]any{"config": map[string]any{"mcp_servers": map[string]any{"fake_docs": map[string]any{"enabled": true}}}, "origins": map[string]any{}})
		case "thread/start", "thread/resume":
			if path := os.Getenv("FAKEAGENT_THREAD_LOG"); path != "" {
				if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
					_, _ = f.Write(append(append([]byte(nil), req.Raw...), '\n'))
					f.Close()
				}
			}
			mu.Lock()
			if req.Params.BaseInstructions != "" {
				system = req.Params.BaseInstructions
			}
			thread = req.Params.ThreadID
			if thread == "" {
				thread = fmt.Sprintf("0199%012d", time.Now().UnixNano()%1e12)
			}
			trusted = req.Params.Config.BypassHookTrust
			mu.Unlock()
			notify("thread/started", map[string]any{"thread": map[string]any{"id": thread}})
			reply(req.ID, map[string]any{"thread": map[string]any{"id": thread}, "model": "gpt-fake-1"})
		case "turn/start":
			mu.Lock()
			if turn != nil {
				mu.Unlock()
				replyErr(req.ID, "a turn is already running")
				continue
			}
			turns++
			t := &codexTurn{id: fmt.Sprintf("turn%d", turns), thread: thread}
			turn = t
			mu.Unlock()
			reply(req.ID, map[string]any{"turn": map[string]any{"id": t.id, "status": "inProgress", "items": []any{}}})
			wg.Add(1)
			go runTurn(t, text)
		case "turn/steer":
			mu.Lock()
			t := turn
			ok := t != nil && t.id == req.Params.ExpectedTurnID && !t.interrupted && os.Getenv("FAKEAGENT_CODEX_REFUSE_STEER") != "1"
			if ok {
				t.steers = append(t.steers, codexSteer{clientID: req.Params.ClientID, text: text})
			}
			mu.Unlock()
			if !ok {
				replyErr(req.ID, "no active turn to steer")
				continue
			}
			reply(req.ID, map[string]any{"turnId": t.id})
		case "turn/interrupt":
			mu.Lock()
			if turn != nil {
				turn.interrupted = true
			}
			mu.Unlock()
			reply(req.ID, map[string]any{})
		default:
			if len(req.ID) > 0 {
				replyErr(req.ID, "fake codex: unknown method "+req.Method)
			}
		}
	}
	wg.Wait()
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
				if toolsOn && strings.Contains(system, fakecli.WorkerRole) {
					wk := newWorker(prompt)
					for i, call := range workerPlan(prompt, cwd) {
						name := strings.ToLower(call.tool)
						emit(map[string]any{"type": "tool_execution_start", "toolCallId": fmt.Sprint(i), "toolName": name, "args": call.input})
						isErr := wk.run(call, runHook(hook, "pi", name, call.input))
						emit(map[string]any{"type": "tool_execution_end", "toolCallId": fmt.Sprint(i), "toolName": name, "isError": isErr})
					}
					delay(true)
					answer = wk.summary()
				} else {
					delay(false)
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
