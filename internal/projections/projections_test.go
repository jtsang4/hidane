package projections_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/projections"
)

var ctx = context.Background()

func m[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func say(k *kernel.Kernel, text string) kernel.Event {
	return m(k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main", Payload: kernel.Payload{"text": text}}))
}

func answer(k *kernel.Kernel, root kernel.Event, text string, extra kernel.Payload) kernel.Event {
	p := kernel.Payload{"text": text, "root": root.ID, "of": root.ID}
	for key, v := range extra {
		p[key] = v
	}
	return m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: p}))
}

func TestSearchFindsEverythingSaidButNeverHiddenWords(t *testing.T) {
	k := kerneltest.New(t)
	a := say(k, "部署 staging 服务器")
	answer(k, a, "staging 已部署", nil)
	b := say(k, "我的密码是 staging-secret")
	m(k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "message.redacted", ThreadID: "main", Payload: kernel.Payload{"of": b.ID}}))
	say(k, "100% done_x")
	page := m(projections.SearchConversation(ctx, k, "staging", nil, 10))
	if len(page.Events) != 2 {
		t.Fatalf("hits: %+v", page.Events)
	}
	for _, e := range page.Events {
		if e.ID == b.ID {
			t.Fatal("a hidden message must never match")
		}
	}
	if got := m(projections.SearchConversation(ctx, k, "部署 已", nil, 10)); len(got.Events) != 1 {
		t.Fatalf("terms AND together: %+v", got.Events)
	}
	if got := m(projections.SearchConversation(ctx, k, "100%", nil, 10)); len(got.Events) != 1 {
		t.Fatal("LIKE wildcards are literal")
	}
	if got := m(projections.SearchConversation(ctx, k, "_", nil, 10)); len(got.Events) != 1 {
		t.Fatal("underscore is literal")
	}
	paged := m(projections.SearchConversation(ctx, k, "staging", nil, 1))
	if !paged.HasMore {
		t.Fatal("hasMore")
	}
	before := paged.Events[0].Seq
	next := m(projections.SearchConversation(ctx, k, "staging", &before, 1))
	if len(next.Events) != 1 || next.Events[0].Seq >= before {
		t.Fatal("paging backwards")
	}
}

func TestRecentConversationIsBoundedAndSkipsHidden(t *testing.T) {
	k := kerneltest.New(t)
	for i := 0; i < 20; i++ {
		q := say(k, strings.Repeat("话", 10))
		answer(k, q, "好", nil)
	}
	hidden := say(k, "删掉我")
	m(k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "message.redacted", ThreadID: "main", Payload: kernel.Payload{"of": hidden.ID}}))
	current := say(k, "现在的问题")
	rc := m(projections.Recent(ctx, k, map[string]bool{current.ID: true}, 0, 0))
	if rc.Turns != projections.ContextTurns || strings.Contains(rc.Text, "删掉我") || strings.Contains(rc.Text, "现在的问题") {
		t.Fatalf("turns=%d text=%s", rc.Turns, rc.Text)
	}
	small := m(projections.Recent(ctx, k, nil, 50, 200))
	if small.Turns >= 20 {
		t.Fatal("the size bound applies")
	}
	days := m(projections.ConversationDays(ctx, k, "Asia/Shanghai"))
	if len(days) != 1 || days[0].Count < 40 {
		t.Fatalf("days: %+v", days)
	}
}

func TestBoardStates(t *testing.T) {
	k := kerneltest.New(t)
	q := say(k, "做个东西")
	idle := m(k.CreateWorkItem(ctx, "idle", "test", kernel.CreateWorkItemOpts{Of: q.ID}))
	waiting := m(k.CreateWorkItem(ctx, "waiting", "test", kernel.CreateWorkItemOpts{}))
	m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "escalation", ThreadID: "main", WorkItemID: waiting.ID, Payload: kernel.Payload{"question": "which?", "reason": "question"}}))
	running := m(k.CreateWorkItem(ctx, "running", "test", kernel.CreateWorkItemOpts{}))
	if err := k.CreateExecution(ctx, "ex_run", running.ID, kernel.ManagerAddress(running.ID)); err != nil {
		t.Fatal(err)
	}
	_ = k.SetExecutionStatus(ctx, "ex_run", kernel.ExecRunning)
	m(k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "execution.started", WorkItemID: running.ID, ExecutionID: "ex_run", Payload: kernel.Payload{"instructions": "do it"}}))
	m(k.Append(ctx, kernel.EventInput{Source: "agent:worker", Kind: "side_effect.intent", WorkItemID: running.ID, ExecutionID: "ex_run", Payload: kernel.Payload{"tool": "bash", "input": `{"command":"ls   -la"}`}}))
	parent := m(k.CreateWorkItem(ctx, "parent", "test", kernel.CreateWorkItemOpts{}))
	m(k.CreateWorkItem(ctx, "child", "test", kernel.CreateWorkItemOpts{ParentID: parent.ID}))
	thinking := m(k.CreateWorkItem(ctx, "thinking", "test", kernel.CreateWorkItemOpts{}))
	m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "user.message", Mailbox: kernel.ManagerAddress(thinking.ID), WorkItemID: thinking.ID}}))

	cards := m(projections.BuildBoard(ctx, k, nil))
	state := map[string]projections.BoardCard{}
	for _, c := range cards {
		state[c.Item.Title] = c
	}
	for title, want := range map[string]string{"idle": "idle", "waiting": "waiting", "running": "running", "parent": "delegated", "thinking": "thinking"} {
		if got := state[title].State; got != want {
			t.Errorf("%s: %s want %s", title, got, want)
		}
	}
	if c := state["running"]; c.Execution == nil || c.Execution.ToolCalls != 1 || *c.Execution.LastTool != "bash ls -la" || c.Execution.Instructions != "do it" {
		t.Fatalf("execution view: %+v", c.Execution)
	}
	if c := state["idle"]; c.Anchor == nil || *c.Anchor != q.ID {
		t.Fatal("anchor")
	}
	if c := state["waiting"]; c.Escalation == nil || c.Escalation.Question != "which?" {
		t.Fatal("escalation")
	}
	if len(state["parent"].ChildIDs) != 1 {
		t.Fatal("children")
	}
	_ = idle
}

func m2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

func TestWorklogProjection(t *testing.T) {
	k := kerneltest.New(t)
	item := m(k.CreateWorkItem(ctx, "报告", "test", kernel.CreateWorkItemOpts{}))
	say(k, "hello")
	secret := say(k, "secret words")
	m(k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "message.redacted", ThreadID: "main", Payload: kernel.Payload{"of": secret.ID}}))
	md, n := m2(projections.RenderDay(ctx, k, k.Today()))
	if n < 4 || !strings.Contains(md, "## Main thread") || !strings.Contains(md, "## "+item.ID+" — 报告 (open)") {
		t.Fatalf("%s", md)
	}
	if strings.Contains(md, "secret words") || !strings.Contains(md, "(hidden by the person)") {
		t.Fatal("the worklog masks hidden messages")
	}
	path := m(projections.WriteDay(ctx, k, k.Today()))
	if b := kernel.ReadTextFile(path); b != md {
		t.Fatal("the written projection equals the rendered one")
	}
	if projections.DescribeTool(kernel.Payload{"tool": "write", "input": `{"path":"a.txt"}`}) != "write a.txt" {
		t.Fatal("describe tool")
	}
}

// Recall dates what was said the way the person lived it, not in UTC.
func TestRecallDatesInLocalTime(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip(err)
	}
	time.Local = loc
	text := projections.DescribeRecall([]kernel.Event{{Seq: 1, Kind: "user.message", TS: "2026-10-01T21:46:00.000Z", Payload: kernel.Payload{"text": "deploy"}}})
	if !strings.Contains(text, "2026-10-02 05:46 person: deploy") {
		t.Fatalf("recall: %q", text)
	}
}
