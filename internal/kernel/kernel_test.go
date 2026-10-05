package kernel_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
)

var ctx = context.Background()

func m[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestEventsAreAppendOnly(t *testing.T) {
	k := kerneltest.New(t)
	ev := m(k.Append(ctx, kernel.EventInput{Source: "test", Kind: "x.y", Payload: kernel.Payload{"a": 1}}))
	if _, err := k.DB.Exec(`UPDATE events SET kind = 'changed' WHERE id = ?`, ev.ID); err == nil {
		t.Fatal("UPDATE on events must be refused")
	}
	if _, err := k.DB.Exec(`DELETE FROM events WHERE id = ?`, ev.ID); err == nil {
		t.Fatal("DELETE on events must be refused")
	}
	got, ok, err := k.GetEvent(ctx, ev.ID)
	if err != nil || !ok || got.Kind != "x.y" {
		t.Fatalf("event changed: %+v %v %v", got, ok, err)
	}
}

func TestListFiltersAndTailOrder(t *testing.T) {
	k := kerneltest.New(t)
	for i := 0; i < 5; i++ {
		m(k.Append(ctx, kernel.EventInput{Source: "test", Kind: "a", ThreadID: "main"}))
		m(k.Append(ctx, kernel.EventInput{Source: "test", Kind: "b", WorkItemID: "wi_1"}))
	}
	tail := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "a", Tail: 3}))
	if len(tail) != 3 || tail[0].Seq > tail[2].Seq {
		t.Fatalf("tail must be the newest 3 in ascending order: %+v", tail)
	}
	both := m(k.ListEvents(ctx, kernel.ListFilter{Kinds: []string{"a", "b"}, Limit: 4}))
	if len(both) != 4 {
		t.Fatalf("kinds OR: %d", len(both))
	}
	item := m(k.ListEvents(ctx, kernel.ListFilter{WorkItemID: "wi_1"}))
	if len(item) != 5 {
		t.Fatalf("work item filter: %d", len(item))
	}
	after := tail[0].Seq
	newer := m(k.ListEvents(ctx, kernel.ListFilter{AfterSeq: &after}))
	for _, e := range newer {
		if e.Seq <= after {
			t.Fatalf("afterSeq leaked %d", e.Seq)
		}
	}
	today := m(k.ListEvents(ctx, kernel.ListFilter{Day: k.Today()}))
	if len(today) != 10 {
		t.Fatalf("day filter: %d", len(today))
	}
}

func TestCursorsAndReplay(t *testing.T) {
	k := kerneltest.New(t)
	for i := 0; i < 3; i++ {
		m(k.Append(ctx, kernel.EventInput{Source: "test", Kind: "n"}))
	}
	batch := m(k.NextBatch(ctx, "c1", 2))
	if len(batch) != 2 {
		t.Fatal(len(batch))
	}
	if err := k.CommitCursor(ctx, "c1", batch[1].Seq); err != nil {
		t.Fatal(err)
	}
	rest := m(k.NextBatch(ctx, "c1", 10))
	if len(rest) != 1 {
		t.Fatalf("after commit: %d", len(rest))
	}
	if err := k.ResetCursor(ctx, "c1", 0); err != nil {
		t.Fatal(err)
	}
	if again := m(k.NextBatch(ctx, "c1", 10)); len(again) != 3 {
		t.Fatalf("replay after reset: %d", len(again))
	}
	if _, ok := m3(k.LookupCursor(ctx, "never")); ok {
		t.Fatal("a consumer that never committed has no cursor")
	}
}

func m3[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

func TestConsumeWakesOnAppendAndDrainsFullBatches(t *testing.T) {
	k := kerneltest.New(t)
	for i := 0; i < 5; i++ {
		m(k.Append(ctx, kernel.EventInput{Source: "test", Kind: "n"}))
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var handled atomic.Int64
	pass := func(ctx context.Context) (int, error) {
		batch, err := k.NextBatch(ctx, "c", 2)
		if err != nil || len(batch) == 0 {
			return 0, err
		}
		handled.Add(int64(len(batch)))
		return len(batch), k.CommitCursor(ctx, "c", batch[len(batch)-1].Seq)
	}
	done := make(chan struct{})
	go func() { k.Consume(cctx, "test", time.Hour, 2, pass); close(done) }()
	waitFor := func(n int64) {
		deadline := time.Now().Add(5 * time.Second)
		for handled.Load() != n {
			if time.Now().After(deadline) {
				t.Fatalf("handled %d, want %d", handled.Load(), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor(5) // full batches run again without waiting for the hour-long poll
	m(k.Append(ctx, kernel.EventInput{Source: "test", Kind: "n"}))
	waitFor(6)
	cancel()
	<-done
}

func TestRedactionIsAReadTimeMask(t *testing.T) {
	k := kerneltest.New(t)
	msg := m(k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main",
		Payload: kernel.Payload{"text": "my password is hunter2", "root": "x"}}))
	fwd := m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "user.message", ThreadID: "th_1",
		Payload: kernel.Payload{"text": "my password is hunter2", "of": msg.ID}}))
	m(k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "message.redacted", ThreadID: "main",
		Payload: kernel.Payload{"of": msg.ID}}))
	for _, id := range []string{msg.ID, fwd.ID} {
		got, _, err := k.GetEvent(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Payload.Str("text") != "" || !got.Payload.Bool("redacted") {
			t.Fatalf("%s not masked: %+v", id, got.Payload)
		}
	}
	got, _, _ := k.GetEvent(ctx, msg.ID)
	if got.Payload.Str("root") != "x" {
		t.Fatal("structural keys must survive masking")
	}
	var raw string
	_ = k.DB.QueryRow(`SELECT payload FROM events WHERE id = ?`, msg.ID).Scan(&raw)
	if !strings.Contains(raw, "hunter2") {
		t.Fatal("the stored row must never be rewritten")
	}
}

func TestPostSeedsCursorAndEnforcesHopBudget(t *testing.T) {
	k := kerneltest.New(t)
	k.Cfg.MaxHops = 2
	first, ok := m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "test", Kind: "m", Mailbox: "primary"}}))
	if !ok || first.Lane != kernel.LaneNormal {
		t.Fatalf("post: %+v %v", first, ok)
	}
	pending := m(k.PendingMessages(ctx, "primary", 10))
	if len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending: %+v", pending)
	}
	second, _ := m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "test", Kind: "m", Mailbox: "primary"}, CausedBy: &first}))
	third, _ := m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "test", Kind: "m", Mailbox: "primary"}, CausedBy: &second}))
	if third.Hop != 2 {
		t.Fatalf("hop: %d", third.Hop)
	}
	_, ok = m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "test", Kind: "m", Mailbox: "primary"}, CausedBy: &third}))
	if ok {
		t.Fatal("hop budget must stop the chain")
	}
	esc := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "escalation"}))
	if len(esc) != 1 || esc[0].Payload.Str("reason") != "budget" || esc[0].Payload.Str("root") != third.ID {
		t.Fatalf("budget escalation: %+v", esc)
	}
}

func m2[A any, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

func TestMailboxesWithPendingPutsInterruptFirst(t *testing.T) {
	k := kerneltest.New(t)
	m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "m", Mailbox: "manager:wi_a"}}))
	m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "m", Mailbox: "primary", Lane: kernel.LaneInterrupt}}))
	list := m(k.MailboxesWithPending(ctx))
	if len(list) != 2 || list[0].Address != "primary" || !list[0].Urgent {
		t.Fatalf("order: %+v", list)
	}
}

func TestRuntimeBatchesAPileUpIntoOneTurn(t *testing.T) {
	k := kerneltest.New(t)
	for i := 0; i < 3; i++ {
		m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "user.message", Mailbox: "primary"}}))
	}
	rt := kernel.NewRuntime(k, 4)
	var turns, seen int
	rt.Register("primary", func(_ context.Context, _ string, msgs []kernel.Event) error {
		turns++
		seen += len(msgs)
		return nil
	})
	if err := rt.Drain(10); err != nil {
		t.Fatal(err)
	}
	if turns != 1 || seen != 3 {
		t.Fatalf("turns=%d seen=%d", turns, seen)
	}
	if p := m(k.PendingMessages(ctx, "primary", 10)); len(p) != 0 {
		t.Fatal("cursor not committed")
	}
}

func TestRuntimeConsumesAPoisonTurnAndRecordsIt(t *testing.T) {
	k := kerneltest.New(t)
	msg, _ := m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "user.message", Mailbox: "primary"}}))
	rt := kernel.NewRuntime(k, 4)
	calls := 0
	rt.Register("primary", func(context.Context, string, []kernel.Event) error {
		calls++
		return errors.New("boom")
	})
	if err := rt.Drain(10); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("a failing turn must not be retried forever: %d", calls)
	}
	errs := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "agent.error"}))
	if len(errs) != 1 || errs[0].Payload.Str("of") != msg.ID {
		t.Fatalf("agent.error: %+v", errs)
	}
}

func TestRuntimeNeverRunsTwoTurnsOfOneMailboxAtOnce(t *testing.T) {
	k := kerneltest.New(t)
	rt := kernel.NewRuntime(k, 4)
	var inFlight, maxInFlight int32
	var mu sync.Mutex
	rt.Register("manager:", func(context.Context, string, []kernel.Event) error {
		n := atomic.AddInt32(&inFlight, 1)
		mu.Lock()
		if n > maxInFlight {
			maxInFlight = n
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return nil
	})
	rt.Start()
	defer rt.Stop()
	for i := 0; i < 5; i++ {
		m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "m", Mailbox: "manager:wi_x"}}))
		time.Sleep(5 * time.Millisecond)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p, _ := k.PendingMessages(ctx, "manager:wi_x", 10); len(p) == 0 && atomic.LoadInt32(&inFlight) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if maxInFlight != 1 {
		t.Fatalf("one mailbox ran %d turns at once", maxInFlight)
	}
}

func TestIdleTasksWaitForQuiet(t *testing.T) {
	k := kerneltest.New(t)
	rt := kernel.NewRuntime(k, 4)
	var idleRuns int32
	block := make(chan struct{})
	rt.Register("primary", func(context.Context, string, []kernel.Event) error {
		<-block
		return nil
	})
	rt.RegisterIdle("idle", func(context.Context) error { atomic.AddInt32(&idleRuns, 1); return nil }, 0, time.Hour)
	m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "t", Kind: "m", Mailbox: "primary"}}))
	rt.Start()
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&idleRuns) != 0 {
		t.Fatal("idle work ran while a turn was busy")
	}
	close(block)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&idleRuns) == 0 && time.Now().Before(deadline) {
		rt.Kick()
		time.Sleep(20 * time.Millisecond)
	}
	rt.Stop()
	if atomic.LoadInt32(&idleRuns) == 0 {
		t.Fatal("idle work never ran once quiet")
	}
}

func TestWorkItemTree(t *testing.T) {
	k := kerneltest.New(t)
	root := m(k.CreateWorkItem(ctx, "root", "test", kernel.CreateWorkItemOpts{Of: "ev_1"}))
	child := m(k.CreateWorkItem(ctx, "child", "test", kernel.CreateWorkItemOpts{ParentID: root.ID}))
	grand := m(k.CreateWorkItem(ctx, "grand", "test", kernel.CreateWorkItemOpts{ParentID: child.ID}))
	if _, err := os.Stat(filepath.Join(root.Workspace, ".hidane", "sessions")); err != nil {
		t.Fatalf("workspace not created: %v", err)
	}
	tree := m(k.Subtree(ctx, root.ID))
	if len(tree) != 3 || tree[0].ID != root.ID || tree[2].ID != grand.ID {
		t.Fatalf("subtree: %+v", tree)
	}
	anc := m(k.Ancestors(ctx, grand))
	if len(anc) != 2 || anc[0].ID != child.ID || anc[1].ID != root.ID {
		t.Fatalf("ancestors: %+v", anc)
	}
	updated := m(k.SetWorkItemStatus(ctx, child.ID, kernel.StatusDone, "test"))
	if updated.Status != "done" {
		t.Fatal(updated.Status)
	}
	changes := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "work_item.status_changed"}))
	if len(changes) != 1 || changes[0].Payload.Str("from") != "open" || changes[0].Payload.Str("to") != "done" {
		t.Fatalf("status fact: %+v", changes)
	}
	created := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "work_item.created", WorkItemID: root.ID}))
	if len(created) != 1 || created[0].Payload.Str("of") != "ev_1" {
		t.Fatalf("created fact: %+v", created)
	}
	files := k.PolicyFilesFor(ctx, grand)
	if len(files) != 4 || files[0] != k.GlobalPolicyPath() || !strings.HasPrefix(files[1], root.Workspace) {
		t.Fatalf("policy chain must run outermost first: %v", files)
	}
}

func TestSchedulesValidateAndStayOnGrid(t *testing.T) {
	k := kerneltest.New(t)
	name, action, prompt := "ping", "prompt", "hello"
	bad := kernel.ScheduleInput{Name: &name, Action: &action, Spec: &kernel.ScheduleSpec{Prompt: prompt}}
	if _, err := k.CreateSchedule(ctx, bad, "test"); err == nil {
		t.Fatal("a schedule without timing must be refused")
	}
	interval := 15
	good := bad
	good.IntervalSec = &interval
	sc := m(k.CreateSchedule(ctx, good, "test"))
	due, _ := kernel.ParseTime(*sc.NextRunAt)
	fired := due.Add(700 * time.Millisecond)
	next := kernel.NextAfterRun(sc, fired)
	if !next.Equal(due.Add(15 * time.Second)) {
		t.Fatalf("next run must be anchored to the due time: %v vs %v", next, due)
	}
	late := kernel.NextAfterRun(sc, due.Add(time.Hour))
	if late.Sub(due.Add(time.Hour)) > 15*time.Second || !late.After(due.Add(time.Hour)) {
		t.Fatalf("downtime must skip missed slots, not replay them: %v", late)
	}
	cronExpr := "0 9 * * *"
	tz := "Asia/Shanghai"
	patched := m(k.UpdateSchedule(ctx, sc.ID, kernel.ScheduleInput{Cron: &cronExpr, Timezone: &tz, IntervalSec: nil, Present: map[string]bool{"cron": true, "intervalSec": true, "timezone": true}}, "test"))
	if patched.IntervalSec != nil || patched.Cron == nil {
		t.Fatalf("switching to cron in one patch: %+v", patched)
	}
	nr, _ := kernel.ParseTime(*patched.NextRunAt)
	loc, _ := time.LoadLocation(tz)
	if nr.In(loc).Hour() != 9 {
		t.Fatalf("cron in timezone: %v", nr.In(loc))
	}
	if err := k.DeleteSchedule(ctx, sc.ID, "test"); err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, e := range m(k.ListEvents(ctx, kernel.ListFilter{PayloadKey: "scheduleId", PayloadValue: sc.ID})) {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "schedule.created,schedule.updated,schedule.deleted" {
		t.Fatalf("schedule facts: %v", kinds)
	}
}

func promoted(e kernel.MemoryEntry, _ bool, err error) kernel.MemoryEntry {
	if err != nil {
		panic(err)
	}
	return e
}

func TestMemoryFilesPromoteParseForget(t *testing.T) {
	k := kerneltest.New(t)
	a := promoted(k.PromoteToFile(ctx, "preference", "prefers concise answers", "global", "", "", "test"))
	promoted(k.PromoteToFile(ctx, "fact", "lives in Shanghai", "global", "", "", "test"))
	promoted(k.PromoteToFile(ctx, "preference", "writes in Chinese", "global", "", "", "test"))
	entries := kernel.ParseMemories(kernel.ReadTextFile(k.GlobalMemoryPath()))
	if len(entries) != 3 {
		t.Fatalf("entries: %+v", entries)
	}
	prefs := 0
	for _, e := range entries {
		if e.Kind == "preference" {
			prefs++
		}
	}
	if prefs != 2 {
		t.Fatalf("section grouping: %+v", entries)
	}
	ok := m(k.Forget(ctx, a.ID, "test"))
	if !ok || len(kernel.ParseMemories(kernel.ReadTextFile(k.GlobalMemoryPath()))) != 2 {
		t.Fatal("forget did not remove the entry")
	}
	if f := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "memory.forgotten"})); len(f) != 1 {
		t.Fatal("forgetting must be recorded as memory.forgotten")
	}
}

// A work item's memory is as forgettable as the global one, by id alone.
func TestWorkItemMemoryCanBeForgotten(t *testing.T) {
	k := kerneltest.New(t)
	item := m(k.CreateWorkItem(ctx, "site", "test", kernel.CreateWorkItemOpts{}))
	k.EnsureWorkspace(item.ID)
	entry := promoted(k.PromoteToFile(ctx, "decision", "deploy from main", "work_item", item.Workspace, item.ID, "test"))
	layers := m(k.WorkItemMemories(ctx))
	if len(layers) != 1 || layers[0].WorkItemID != item.ID || len(layers[0].Entries) != 1 || layers[0].Entries[0].ID != entry.ID {
		t.Fatalf("layers: %+v", layers)
	}
	if !m(k.Forget(ctx, entry.ID, "test")) {
		t.Fatal("a work item memory must be forgettable by id")
	}
	if len(m(k.WorkItemMemories(ctx))) != 0 {
		t.Fatal("the entry is still in the work item's MEMORY.md")
	}
	f := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "memory.forgotten"}))
	if len(f) != 1 || f[0].WorkItemID != item.ID {
		t.Fatalf("forgotten: %+v", f)
	}
	if m(k.Forget(ctx, entry.ID, "test")) {
		t.Fatal("forgetting twice finds nothing")
	}
}

// A replayed distillation finds what it promoted before instead of adding it again.
func TestPromotionIsIdempotent(t *testing.T) {
	k := kerneltest.New(t)
	a, added, err := k.PromoteToFile(ctx, "preference", "prefers tables", "global", "", "", "test")
	if err != nil || !added {
		t.Fatal(err)
	}
	b, again, err := k.PromoteToFile(ctx, "preference", "prefers tables", "global", "", "", "test")
	if err != nil || again {
		t.Fatalf("the second promotion must report nothing added: %v %v", again, err)
	}
	if a.ID != b.ID || len(kernel.ParseMemories(kernel.ReadTextFile(k.GlobalMemoryPath()))) != 1 {
		t.Fatalf("promoted twice: %+v %+v", a, b)
	}
	if p := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "memory.promoted"})); len(p) != 1 {
		t.Fatalf("nothing new was promoted the second time: %d events", len(p))
	}
}

func TestResolveInsideRefusesEscapes(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, "ok.txt"), []byte("hi"), 0o644)
	outside := t.TempDir()
	_ = os.Symlink(outside, filepath.Join(ws, "link"))
	for _, p := range []string{"../etc/passwd", "/etc/passwd", "a/../../x", "link/secret"} {
		if got := kernel.ResolveInside(ws, p); got != "" {
			t.Fatalf("%q escaped to %q", p, got)
		}
	}
	if kernel.ResolveInside(ws, "ok.txt") == "" {
		t.Fatal("a file inside must resolve")
	}
	c := kernel.ReadArtifact(ws, "ok.txt")
	if c == nil || c.Text == nil || *c.Text != "hi" {
		t.Fatalf("read: %+v", c)
	}
}

func TestTriageRulesAndLoopProtection(t *testing.T) {
	hb := kernel.Event{Source: "connector:timer", Kind: "connector.heartbeat"}
	if _, action := kernel.TriageEvent(hb, kernel.DefaultTriageRules); action != "record" {
		t.Fatal("heartbeat must be record-only")
	}
	wh := kernel.Event{Source: "connector:webhook:gh", Kind: "connector.webhook"}
	if _, action := kernel.TriageEvent(wh, kernel.DefaultTriageRules); action != "wake_primary" {
		t.Fatal("webhook must wake")
	}
	if kernel.NeedsTriage(kernel.Event{Source: "agent:primary", Kind: "connector.webhook"}) {
		t.Fatal("agent events must never re-enter triage")
	}
	if !kernel.NeedsTriage(wh) {
		t.Fatal("connector events need triage")
	}
}

func TestRunNowDoesNotPostponeTheSchedule(t *testing.T) {
	k := kerneltest.New(t)
	name, action, interval := "hourly", "prompt", 3600
	sc := m(k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &interval, Spec: &kernel.ScheduleSpec{Prompt: "x"}}, "test"))
	if err := k.MarkRun(ctx, sc.ID, "posted", time.Now()); err != nil {
		t.Fatal(err)
	}
	after := m(k.GetSchedule(ctx, sc.ID))
	if *after.NextRunAt != *sc.NextRunAt {
		t.Fatalf("a manual run moved the next run from %s to %s", *sc.NextRunAt, *after.NextRunAt)
	}
}

// A database from before work items could pin an agent opens with the column
// added and its items intact; its unused deadline_at column does no harm.
func TestOpenAddsColumnsToAnOlderDatabase(t *testing.T) {
	cfg := config.ForTest(t.TempDir())
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(cfg.DBPath()))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE work_items (id TEXT PRIMARY KEY, title TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'open',
			workspace TEXT NOT NULL, thread_id TEXT NOT NULL, parent_id TEXT, deadline_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO work_items (id, title, workspace, thread_id, created_at, updated_at) VALUES ('wi_old', 'old', '/tmp/x', 'th_old', 'now', 'now')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	k, err := kernel.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	item := m(k.GetWorkItem(ctx, "wi_old"))
	if item.Title != "old" || item.RunAs != nil {
		t.Fatalf("old item: %+v", item)
	}
	if got := m(k.SetWorkItemRunAs(ctx, "wi_old", &kernel.RunAs{Agent: "pi"}, "test")); got.RunAs == nil || got.RunAs.Agent != "pi" {
		t.Fatalf("pinned: %+v", got.RunAs)
	}
}
