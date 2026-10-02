package agents_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agentcli/fakecli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/settings"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "guard" {
		os.Exit(guard.RunHook(os.Args[3], os.Stdin, os.Stdout, os.Stderr, guard.EnvFromOS()))
	}
	os.Exit(m.Run())
}

var ctx = context.Background()

func m[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type world struct {
	t  *testing.T
	k  *kernel.Kernel
	s  *agents.System
	rt *kernel.Runtime
}

func newWorld(t *testing.T, agent string, extraEnv ...string) *world {
	t.Helper()
	k := kerneltest.New(t)
	dir := fakecli.Dir(t)
	st := m(settings.Load(k.Cfg.SettingsPath()))
	m(st.SetRoles(map[string]settings.RoleConfig{
		"primary": {Agent: agent}, "manager": {Agent: agent}, "worker": {Agent: agent}, "distiller": {Agent: agent},
	}))
	self, _ := os.Executable()
	env := append(agentcli.WithoutVars(os.Environ(), "PATH"), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	env = append(env, extraEnv...)
	l := &agentcli.Launcher{
		Binary:       func(a string) (string, error) { return filepath.Join(dir, a), nil },
		GuardCommand: self, RuntimeDir: k.Cfg.RuntimeDir(), Env: env,
	}
	if err := l.EnsureRuntimeFiles(); err != nil {
		t.Fatal(err)
	}
	s := agents.New(k, st, l)
	return &world{t: t, k: k, s: s, rt: s.NewRuntime()}
}

// settle runs turns and waits for workers until nothing is left to do.
func (w *world) settle() {
	w.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := w.rt.Drain(50); err != nil {
			w.t.Fatal(err)
		}
		w.s.Pool.Wait()
		pending := m(w.k.MailboxesWithPending(ctx))
		st := w.s.Pool.Status()
		if len(pending) == 0 && st["running"] == 0 && st["queued"] == 0 {
			return
		}
	}
	w.t.Fatal("the system never settled")
}

func (w *world) events(kind string) []kernel.Event {
	return m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: kind}))
}

func (w *world) kinds() []string {
	var out []string
	for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{})) {
		out = append(out, e.Kind)
	}
	return out
}

func indexOf(list []string, kind string, from int) int {
	for i := from; i < len(list); i++ {
		if list[i] == kind {
			return i
		}
	}
	return -1
}

func TestFullLoopOnEveryCLI(t *testing.T) {
	for _, agent := range settings.Agents {
		t.Run(agent, func(t *testing.T) {
			w := newWorld(t, agent)
			msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "创建一个文件写上 good morning", Source: "connector:web"}))
			if msg.Mailbox != kernel.Primary || msg.Lane != kernel.LaneInterrupt {
				t.Fatalf("a person's message is an interrupt-lane message to the Primary: %+v", msg)
			}
			w.settle()

			items := m(w.k.ListWorkItems(ctx, ""))
			if len(items) != 1 {
				t.Fatalf("one work item: %+v", items)
			}
			item := items[0]
			b, err := os.ReadFile(filepath.Join(item.Workspace, "result.txt"))
			if err != nil || !strings.Contains(string(b), "good morning") {
				t.Fatalf("the worker must produce the artifact in the workspace: %q %v", b, err)
			}
			task, _ := os.ReadFile(filepath.Join(item.Workspace, "TASK.md"))
			if !strings.Contains(string(task), "目标") {
				t.Fatalf("TASK.md carries the manager's understanding: %q", task)
			}
			chain := []string{"user.message", "route.decision", "message.attributed", "user.message", "manager.decision",
				"execution.started", "execution.running", "side_effect.intent", "side_effect.result", "execution.finished", "manager.decision", "agent.reply"}
			kinds := w.kinds()
			pos := 0
			for _, k := range chain {
				i := indexOf(kinds, k, pos)
				if i < 0 {
					t.Fatalf("missing %s after position %d in %v", k, pos, kinds)
				}
				pos = i + 1
			}
			attributed := w.events("message.attributed")[0]
			if !attributed.Payload.Bool("created") || attributed.Payload.Str("by") != "model" {
				t.Fatalf("attribution: %+v", attributed.Payload)
			}
			finished := w.events("execution.finished")[0]
			if finished.Mailbox != kernel.ManagerAddress(item.ID) || !finished.Payload.Bool("ok") {
				t.Fatalf("the outcome goes back to the owner: %+v", finished)
			}
			for _, e := range w.events("agent.reply") {
				if e.Payload.Str("root") != msg.ID {
					t.Fatalf("every answer names the message it answers: %+v", e.Payload)
				}
			}
			replies := w.events("agent.reply")
			if !strings.Contains(replies[len(replies)-1].Payload.Str("text"), "已完成") {
				t.Fatalf("the manager reports the result: %+v", replies)
			}
			ex := m(w.k.ActiveExecutions(ctx))
			if len(ex) != 0 {
				t.Fatalf("no execution may stay active: %+v", ex)
			}
			if _, err := os.Stat(filepath.Join(item.Workspace, ".hidane", "sessions", "manager", "session.json")); err != nil {
				t.Fatal("the manager session is kept for continuity")
			}
		})
	}
}

func TestGreetingIsAnsweredWithoutAWorkItem(t *testing.T) {
	w := newWorld(t, settings.Claude)
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好", Source: "connector:web"}))
	w.settle()
	replies := w.events("agent.reply")
	if len(replies) != 1 || replies[0].Payload.Str("of") != msg.ID || replies[0].ThreadID != "main" {
		t.Fatalf("reply: %+v", replies)
	}
	if len(m(w.k.ListWorkItems(ctx, ""))) != 0 {
		t.Fatal("small talk creates no work item")
	}
}

// A status change answers for itself; the model's reply would say it twice.
func TestStatusChangeIsConfirmedOnce(t *testing.T) {
	w := newWorld(t, settings.Claude)
	m(w.k.CreateWorkItem(ctx, "a", "test", kernel.CreateWorkItemOpts{}))
	m(w.k.CreateWorkItem(ctx, "b", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "把工作项全部关闭", Source: "connector:web"}))
	w.settle()
	var replies []string
	for _, r := range w.events("agent.reply") {
		if r.Payload.Str("root") == msg.ID {
			replies = append(replies, r.Payload.Str("text"))
		}
	}
	if len(replies) != 1 || !strings.Contains(replies[0], "已将 2 个工作项的状态设为 closed") {
		t.Fatalf("one factual confirmation: %q", replies)
	}
}

// A Manager turn that only restates its understanding is asked once more for
// a decision, rather than leaving the task silently idle.
func TestUnderstandingOnlyTurnIsNudgedOnce(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "nudge", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "ONLY_UNDERSTAND 写一个文件 nudged", Source: "connector:web", Target: item.ID}))
	w.settle()
	decisions := w.events("manager.decision")
	if len(decisions) < 2 || decisions[0].Payload.Bool("nudged") || !decisions[1].Payload.Bool("nudged") {
		t.Fatalf("the follow-up is recorded as its own decision: %+v", decisions)
	}
	if started := w.events("execution.started"); len(started) != 1 {
		t.Fatalf("the nudge must produce the missing dispatch: %v", w.kinds())
	}
}

// A long answer reaches the person whole: a silent cut reads as the full answer.
func TestLongManagerReplyIsNotCut(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "long", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "LONG_REPLY please", Source: "connector:web", Target: item.ID}))
	w.settle()
	for _, r := range w.events("agent.reply") {
		if r.WorkItemID == item.ID {
			if text := r.Payload.Str("text"); !strings.HasSuffix(text, "结尾") || len([]rune(text)) != 11004 {
				t.Fatalf("reply was cut: %d runes", len([]rune(text)))
			}
			return
		}
	}
	t.Fatalf("no reply: %v", w.kinds())
}

// Output that is not the effect list gets the same single follow-up as an
// understanding-only turn; it reaches the person only if the retry fails too.
func TestNonEffectOutputIsRetriedBeforeItIsAnswered(t *testing.T) {
	w := newWorld(t, settings.Claude)
	junk := m(w.k.CreateWorkItem(ctx, "junk", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "JUNK_ONCE 写一个文件", Source: "connector:web", Target: junk.ID}))
	w.settle()
	// A Manager that answers in plain text every time.
	pw := newWorld(t, settings.Claude, "FAKEAGENT_MANAGER_PLAIN=1")
	plain := m(pw.k.CreateWorkItem(ctx, "plain", "test", kernel.CreateWorkItemOpts{}))
	m(pw.s.SubmitMessage(ctx, agents.InboundMessage{Text: "问一句", Source: "connector:web", Target: plain.ID}))
	pw.settle()
	started := 0
	for _, e := range w.events("execution.started") {
		if e.WorkItemID == junk.ID {
			started++
		}
	}
	for _, r := range w.events("agent.reply") {
		if r.Payload.Str("text") == "response." {
			t.Fatal("junk output reached the person")
		}
	}
	var plainReplies []string
	for _, r := range pw.events("agent.reply") {
		if r.WorkItemID == plain.ID {
			plainReplies = append(plainReplies, r.Payload.Str("text"))
		}
	}
	if started != 1 {
		t.Fatalf("the retry's dispatch must run: %v", w.kinds())
	}
	if len(plainReplies) != 1 || plainReplies[0] != "我直接回答：这是纯文本。" {
		t.Fatalf("plain text twice is the answer: %q", plainReplies)
	}
}

// The Primary, too, asks once more before stray output becomes the answer.
func TestPrimaryRetriesOutputThatIsNotTheEffectList(t *testing.T) {
	w := newWorld(t, settings.Claude)
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好 JUNK_PRIMARY", Source: "connector:web"}))
	w.settle()
	replies := w.events("agent.reply")
	if len(replies) != 1 || replies[0].Payload.Str("of") != msg.ID || replies[0].Payload.Str("text") != "你好！我是 hidane 的主代理。" {
		t.Fatalf("replies: %+v", replies)
	}
	decisions := w.events("route.decision")
	if len(decisions) != 2 || !decisions[1].Payload.Bool("nudged") {
		t.Fatalf("both answers are on record: %+v", decisions)
	}
}

func TestExplicitTargetSkipsThePrimary(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "notes", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "写 notes", Source: "connector:web", Target: item.ID, Focus: true}))
	w.settle()
	if len(w.events("route.decision")) != 0 {
		t.Fatal("an addressed message needs no routing model call")
	}
	att := w.events("message.attributed")
	if len(att) != 1 || att[0].Payload.Str("by") != "focus" || att[0].Payload.Str("of") != msg.ID {
		t.Fatalf("attribution: %+v", att)
	}
}

func TestSteeringReachesTheRunningWorker(t *testing.T) {
	w := newWorld(t, settings.Pi, "FAKEAGENT_DELAY_MS=1500")
	item := m(w.k.CreateWorkItem(ctx, "long job", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "start", Source: "connector:web", Target: item.ID}))
	if err := w.rt.Drain(20); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ex, ok, _ := w.k.ActiveExecutionFor(ctx, item.ID)
		if ok && ex.Status == kernel.ExecRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("execution never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "also add a footer", Source: "connector:web", Target: item.ID}))
	w.settle()
	steered := w.events("execution.steered")
	if len(steered) != 1 || steered[0].Payload.Str("text") != "also add a footer" {
		t.Fatalf("steered: %+v (kinds %v)", steered, w.kinds())
	}
	finished := w.events("execution.finished")
	if len(finished) != 1 || !strings.Contains(finished[0].Payload.Str("summary"), "also add a footer") {
		t.Fatalf("the worker read the steered words: %+v", finished)
	}
	// Hiding the person's words hides the steered copy as well.
	msgs := w.events("user.message")
	steeredFrom := steered[0].Payload.Str("of")
	var original kernel.Event
	for _, e := range msgs {
		if e.ID == steeredFrom {
			original = e
		}
	}
	if original.ThreadID != "main" {
		t.Fatalf("execution.steered names the person's own message: %+v", steered[0].Payload)
	}
	m(w.s.RedactMessage(ctx, original.ID, "test"))
	if again := w.events("execution.steered"); again[0].Payload.Str("text") != "" || !again[0].Payload.Bool("redacted") {
		t.Fatalf("the steered copy must be masked: %+v", again[0].Payload)
	}
}

// Words that reach an execution as it ends are not an error: they ride with
// its outcome to the Manager.
func TestWordsForAnEndingRunReachTheManagerWithItsOutcome(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "ending job", "test", kernel.CreateWorkItemOpts{}))
	// Recorded as running, but no longer held by the pool: the run has ended
	// and its outcome is not posted yet.
	if err := w.k.CreateExecution(ctx, "ex_ending", item.ID, kernel.ManagerAddress(item.ID)); err != nil {
		t.Fatal(err)
	}
	if err := w.k.SetExecutionStatus(ctx, "ex_ending", kernel.ExecRunning); err != nil {
		t.Fatal(err)
	}
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "also add a footer", Source: "connector:web", Target: item.ID}))
	if err := w.rt.Drain(20); err != nil {
		t.Fatal(err)
	}
	steered := w.events("execution.steered")
	if len(steered) != 1 || !steered[0].Payload.Bool("late") || steered[0].Payload.Str("root") != msg.ID {
		t.Fatalf("steered: %+v (kinds %v)", steered, w.kinds())
	}
	if errs := w.events("agent.error"); len(errs) != 0 {
		t.Fatalf("an ending run is not a delivery error: %+v", errs)
	}
	if err := w.k.SetExecutionStatus(ctx, "ex_ending", kernel.ExecDone); err != nil {
		t.Fatal(err)
	}
	m2, _, err := w.k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "agent:worker", Kind: "execution.finished",
		Mailbox: kernel.ManagerAddress(item.ID), ThreadID: item.ThreadID, WorkItemID: item.ID, ExecutionID: "ex_ending",
		Payload: kernel.Payload{"ok": true, "summary": "done", "root": msg.ID}}, AlwaysDeliver: true})
	if err != nil || m2.ID == "" {
		t.Fatal(err)
	}
	w.settle()
	replies := w.events("agent.reply")
	if len(replies) == 0 || !strings.Contains(replies[len(replies)-1].Payload.Str("text"), "also add a footer") {
		t.Fatalf("the manager was given the late words with the outcome: %+v", replies)
	}
}

// A child that answers and closes in one turn hands its parent that answer,
// whatever order the model listed the two in.
func TestParentHearsTheChildsFinalAnswer(t *testing.T) {
	w := newWorld(t, settings.Claude)
	parent := m(w.k.CreateWorkItem(ctx, "parent", "test", kernel.CreateWorkItemOpts{}))
	child := m(w.k.CreateWorkItem(ctx, "child", "test", kernel.CreateWorkItemOpts{ParentID: parent.ID}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "写一个文件 child-answer", Source: "connector:web", Target: child.ID}))
	w.settle()
	if c := m(w.k.GetWorkItem(ctx, child.ID)); c.Status != kernel.StatusDone {
		t.Fatalf("child status: %s", c.Status)
	}
	settled := w.events("children.settled")
	if len(settled) != 1 {
		t.Fatalf("settled: %+v", settled)
	}
	list, _ := settled[0].Payload["children"].([]any)
	first, _ := list[0].(map[string]any)
	if result, _ := first["result"].(string); !strings.Contains(result, "已完成") {
		t.Fatalf("the parent gets the child's final reply, not an earlier one: %+v", first)
	}
	var childReply, settledAt int64
	for _, r := range w.events("agent.reply") {
		if r.WorkItemID == child.ID {
			childReply = r.Seq
		}
	}
	settledAt = settled[0].Seq
	if childReply == 0 || childReply > settledAt {
		t.Fatalf("the child's reply precedes children.settled: reply %d, settled %d", childReply, settledAt)
	}
}

func TestCancelTreeStopsARunningWorker(t *testing.T) {
	w := newWorld(t, settings.Claude, "FAKEAGENT_DELAY_MS=20000")
	parent := m(w.k.CreateWorkItem(ctx, "parent", "test", kernel.CreateWorkItemOpts{}))
	child := m(w.k.CreateWorkItem(ctx, "child", "test", kernel.CreateWorkItemOpts{ParentID: parent.ID}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "work", Source: "connector:web", Target: child.ID}))
	if err := w.rt.Drain(20); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ex, ok, _ := w.k.ActiveExecutionFor(ctx, child.ID); ok && ex.Status == kernel.ExecRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("execution never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	start := time.Now()
	stopped := m(w.s.Pool.CancelTree(ctx, parent.ID, "cancelled from test", "test"))
	if len(stopped) != 1 || stopped[0] != child.ID {
		t.Fatalf("cancel flows down the tree: %v", stopped)
	}
	for len(w.events("execution.finished")) == 0 {
		if time.Since(start) > 10*time.Second {
			t.Fatal("a cancel must not wait out the run")
		}
		time.Sleep(50 * time.Millisecond)
	}
	w.settle()
	if c := m(w.k.GetWorkItem(ctx, child.ID)); c.Status != kernel.StatusClosed {
		t.Fatalf("a cancelled tree closes its open parts: %s", c.Status)
	}
	if settled := w.events("children.settled"); len(settled) != 0 {
		t.Fatalf("a stopped tree does not settle into a re-plan: %+v", settled)
	}
	if started := w.events("execution.started"); len(started) != 1 {
		t.Fatalf("nothing is dispatched after the person stopped the tree: %v", w.kinds())
	}
	finished := w.events("execution.finished")
	if len(finished) != 1 || !finished[0].Payload.Bool("cancelled") {
		t.Fatalf("finished: %+v", finished)
	}
	cancelled := w.events("execution.cancelled")
	if len(cancelled) != 1 || cancelled[0].Payload.Str("via") != parent.ID || cancelled[0].Seq > finished[0].Seq {
		t.Fatalf("intent before effect: %+v", cancelled)
	}
	cancelledAck := false
	for _, r := range w.events("agent.reply") {
		if r.WorkItemID == child.ID && r.Payload.Str("text") == "执行已取消。" {
			cancelledAck = true
		}
	}
	if !cancelledAck {
		t.Fatalf("a cancelled run is acknowledged without a model call: %+v", w.events("agent.reply"))
	}
}

func TestRestartReportsLostExecutions(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "x", "test", kernel.CreateWorkItemOpts{}))
	if err := w.k.CreateExecution(ctx, "ex_lost01", item.ID, kernel.ManagerAddress(item.ID)); err != nil {
		t.Fatal(err)
	}
	n := m(w.s.Pool.Recover(ctx))
	if n != 1 {
		t.Fatalf("lost: %d", n)
	}
	finished := w.events("execution.finished")
	if len(finished) != 1 || !finished[0].Payload.Bool("lost") || finished[0].Mailbox != kernel.ManagerAddress(item.ID) {
		t.Fatalf("the owner hears about the loss: %+v", finished)
	}
	ex, _, _ := w.k.GetExecution(ctx, "ex_lost01")
	if ex.Status != kernel.ExecLost {
		t.Fatal(ex.Status)
	}
}

func TestPolicyRefusalsAreRecorded(t *testing.T) {
	w := newWorld(t, settings.Codex)
	m(w.k.AddGlobalRule(`result\.txt`, "no result files here", nil))
	item := m(w.k.CreateWorkItem(ctx, "guarded", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "write it", Source: "connector:web", Target: item.ID}))
	w.settle()
	if _, err := os.Stat(filepath.Join(item.Workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatal("the refused write must not happen")
	}
	blocked := w.events("policy.blocked")
	if len(blocked) != 1 || blocked[0].Payload.Str("reason") != "no result files here" || blocked[0].Payload.Str("rule") == "" {
		t.Fatalf("policy.blocked: %+v", blocked)
	}
	finished := w.events("execution.finished")[0]
	if list, _ := finished.Payload["policyBlocks"].([]any); len(list) != 1 {
		t.Fatalf("the outcome carries the refusal: %+v", finished.Payload)
	}
}

func TestRoutingFailureIsReportedNotSwallowed(t *testing.T) {
	w := newWorld(t, settings.Claude)
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "FAKE_FAIL this", Source: "connector:web"}))
	w.settle()
	replies := w.events("agent.reply")
	if len(replies) != 1 || !strings.HasPrefix(replies[0].Payload.Str("text"), "primary routing failed") || replies[0].Payload.Str("of") != msg.ID {
		t.Fatalf("%+v", replies)
	}
}

func TestDistillerPromotesDurableMemory(t *testing.T) {
	w := newWorld(t, settings.Claude)
	for i := 0; i < 2; i++ {
		m(w.k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main", Payload: kernel.Payload{"text": "请记住我喜欢简洁"}}))
	}
	res := m(w.s.RunDistillation(ctx, 1))
	if res.Promoted != 1 {
		t.Fatalf("%+v", res)
	}
	entries := kernel.ParseMemories(kernel.ReadTextFile(w.k.GlobalMemoryPath()))
	if len(entries) != 1 || entries[0].Kind != "preference" {
		t.Fatalf("%+v", entries)
	}
	again := m(w.s.RunDistillation(ctx, 1))
	if !again.Skipped || again.Scanned != 0 && again.Meaningful != 0 {
		t.Fatalf("the cursor advanced past distilled material: %+v", again)
	}
}

// Replay (a reset cursor) must not promote a work item's memories again, even
// when the model words them differently the second time.
func TestDistillerReplayDoesNotDuplicateWorkItemMemory(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "acme", "test", kernel.CreateWorkItemOpts{}))
	m(w.k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: item.ThreadID, WorkItemID: item.ID,
		Payload: kernel.Payload{"text": "任务约定：部署走main分支"}}))
	first := m(w.s.RunDistillation(ctx, 1))
	if first.Promoted != 1 {
		t.Fatalf("first pass: %+v", first)
	}
	if err := w.k.ResetCursor(ctx, "distiller", 0); err != nil {
		t.Fatal(err)
	}
	replay := m(w.s.RunDistillation(ctx, 1))
	if replay.Promoted != 0 {
		t.Fatalf("the replay promoted again: %+v", replay)
	}
	if entries := kernel.ParseMemories(kernel.ReadTextFile(kernel.WorkItemMemoryPath(item.Workspace))); len(entries) != 1 {
		t.Fatalf("work item memories after replay: %+v", entries)
	}
}

// Every answer names the message it is about — a passed deadline included.
func TestDeadlineEscalationCarriesItsRoot(t *testing.T) {
	w := newWorld(t, settings.Claude)
	item := m(w.k.CreateWorkItem(ctx, "report", "test", kernel.CreateWorkItemOpts{}))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "写一个文件 deadline", Source: "connector:web", Target: item.ID}))
	w.settle()
	m(w.k.SetWorkItemDeadline(ctx, item.ID, kernel.FormatTime(time.Now().Add(-time.Minute)), "test"))
	if err := w.s.EnforceDeadlines(ctx); err != nil {
		t.Fatal(err)
	}
	var esc []kernel.Event
	for _, e := range w.events("escalation") {
		if e.Payload.Str("reason") == "deadline" {
			esc = append(esc, e)
		}
	}
	if len(esc) != 1 || esc[0].Payload.Str("root") != msg.ID || esc[0].Payload.Bool("stopped") {
		t.Fatalf("deadline escalation root: %+v (message %s)", esc, msg.ID)
	}
	direct := m(w.k.CreateWorkItem(ctx, "opened directly", "test", kernel.CreateWorkItemOpts{}))
	m(w.k.SetWorkItemDeadline(ctx, direct.ID, kernel.FormatTime(time.Now().Add(-time.Minute)), "test"))
	if err := w.s.EnforceDeadlines(ctx); err != nil {
		t.Fatal(err)
	}
	for _, e := range w.events("escalation") {
		if e.WorkItemID == direct.ID && e.Payload.Str("root") == "" {
			t.Fatalf("an item with no message still roots its notice: %+v", e)
		}
	}
}

func TestHopBudgetStopsRunawayChains(t *testing.T) {
	w := newWorld(t, settings.Claude)
	w.k.Cfg.MaxHops = 1
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "do a thing", Source: "connector:web"}))
	w.settle()
	esc := w.events("escalation")
	found := false
	for _, e := range esc {
		if e.Payload.Str("reason") == "budget" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the person is asked once the budget is spent: %v", w.kinds())
	}
}

func TestBudgetStopsDispatchButNeverAnOutcome(t *testing.T) {
	w := newWorld(t, settings.Claude)
	w.k.Cfg.MaxHops = 3
	item := m(w.k.CreateWorkItem(ctx, "edge", "test", kernel.CreateWorkItemOpts{}))
	cause := m(w.k.Append(ctx, kernel.EventInput{Source: "t", Kind: "user.message", WorkItemID: item.ID, Hop: 3}))
	if _, err := w.s.Pool.Dispatch(ctx, item, kernel.ManagerAddress(item.ID), "do it", "", cause); !errors.Is(err, agents.ErrBudget) {
		t.Fatalf("a dispatch past the budget must not spend anything: %v", err)
	}
	if n := m(w.k.CountExecutions(ctx, item.ID)); n != 0 {
		t.Fatalf("executions: %d", n)
	}
	// An execution started right at the limit still reports to its owner.
	started := m(w.k.Append(ctx, kernel.EventInput{Source: "t", Kind: "execution.started", WorkItemID: item.ID, ExecutionID: "ex_edge", Hop: 3}))
	_ = started
	if err := w.k.CreateExecution(ctx, "ex_edge", item.ID, kernel.ManagerAddress(item.ID)); err != nil {
		t.Fatal(err)
	}
	m(w.s.Pool.Recover(ctx))
	finished := w.events("execution.finished")
	if len(finished) != 1 || finished[0].Mailbox != kernel.ManagerAddress(item.ID) {
		t.Fatalf("the outcome must reach its owner whatever the budget: %+v", finished)
	}
}

func TestShutdownReportsRunningWorkersAsLost(t *testing.T) {
	w := newWorld(t, settings.Claude, "FAKEAGENT_DELAY_MS=30000")
	item := m(w.k.CreateWorkItem(ctx, "long", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "work", Source: "connector:web", Target: item.ID}))
	if err := w.rt.Drain(20); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ex, ok, _ := w.k.ActiveExecutionFor(ctx, item.ID); ok && ex.Status == kernel.ExecRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("execution never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	start := time.Now()
	w.s.Pool.Shutdown(10 * time.Second)
	if time.Since(start) > 8*time.Second {
		t.Fatal("shutdown must stop the CLI, not wait it out")
	}
	finished := w.events("execution.finished")
	if len(finished) != 1 || !finished[0].Payload.Bool("lost") {
		t.Fatalf("the owner hears the execution was lost: %+v", finished)
	}
	if active := m(w.k.ActiveExecutions(ctx)); len(active) != 0 {
		t.Fatal("nothing may stay active")
	}
}

func TestAnInterruptedTurnKeepsItsMessages(t *testing.T) {
	w := newWorld(t, settings.Claude, "FAKEAGENT_DELAY_MS=20000")
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "route me", Source: "connector:web"}))
	w.rt.Start()
	time.Sleep(500 * time.Millisecond)
	w.rt.Stop()
	if p := m(w.k.PendingMessages(ctx, kernel.Primary, 10)); len(p) != 1 {
		t.Fatalf("a turn cut short by shutdown must run again after restart: %d pending", len(p))
	}
	if errs := w.events("agent.error"); len(errs) != 0 {
		t.Fatalf("stopping is not a failure: %+v", errs)
	}
	if replies := w.events("agent.reply"); len(replies) != 0 {
		t.Fatalf("no reply from an aborted turn: %+v", replies)
	}
}

func TestDistillerReadsPastNoise(t *testing.T) {
	w := newWorld(t, settings.Claude)
	for i := 0; i < 195; i++ {
		m(w.k.Append(ctx, kernel.EventInput{Source: "connector:timer", Kind: "connector.heartbeat"}))
	}
	for i := 0; i < 30; i++ {
		m(w.k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main", Payload: kernel.Payload{"text": "请记住我喜欢简洁"}}))
	}
	res := m(w.s.RunDistillation(ctx, 10))
	if res.Skipped || res.Meaningful < 10 || res.Promoted != 1 {
		t.Fatalf("material beyond the first window must be reached: %+v", res)
	}
}

func TestTheExecutionBudgetResumesWhenAnswered(t *testing.T) {
	w := newWorld(t, settings.Claude)
	w.k.Cfg.MaxExecutionsPerItem = 1
	item := m(w.k.CreateWorkItem(ctx, "two rounds", "test", kernel.CreateWorkItemOpts{}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "do it AGAIN", Source: "connector:web", Target: item.ID}))
	w.settle()
	var budget []kernel.Event
	for _, e := range w.events("escalation") {
		if e.Payload.Str("reason") == "budget" {
			budget = append(budget, e)
		}
	}
	if len(budget) != 1 || m(w.k.CountExecutions(ctx, item.ID)) != 1 {
		t.Fatalf("the second dispatch must be refused: %+v", budget)
	}
	for _, r := range w.events("agent.reply") {
		if strings.Contains(r.Payload.Str("text"), "已派出第二个 worker") {
			t.Fatal("a reply announcing a refused dispatch must not reach the person")
		}
	}
	// Answering the escalation starts a new stretch of work.
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "继续", Source: "connector:web", ReplyTo: budget[0].ID}))
	w.settle()
	if n := m(w.k.CountExecutions(ctx, item.ID)); n != 2 {
		t.Fatalf("replying to the budget escalation must let the item continue: %d executions", n)
	}
}
