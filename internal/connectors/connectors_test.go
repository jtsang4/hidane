package connectors_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/connectors"
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

func TestTriageWakesOnlyForDeclaredEvents(t *testing.T) {
	k := kerneltest.New(t)
	m(k.Append(ctx, kernel.EventInput{Source: "connector:timer", Kind: "connector.heartbeat"}))
	hook := m(k.Append(ctx, kernel.EventInput{Source: "connector:webhook:gh", Kind: "connector.webhook", Payload: kernel.Payload{"name": "gh"}}))
	m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "connector.webhook"}))
	m(k.Append(ctx, kernel.EventInput{Source: "connector:schedule", Kind: "connector.http", Payload: kernel.Payload{"wake": true}}))
	handled, woke, err := connectors.TriageOnce(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	if handled != 4 || woke != 2 {
		t.Fatalf("handled=%d woke=%d", handled, woke)
	}
	wakes := m(k.PendingMessages(ctx, kernel.Primary, 10))
	if len(wakes) != 2 || wakes[0].Payload.Str("of") != hook.ID || wakes[0].CausedBy != hook.ID {
		t.Fatalf("a wake is a message to the Primary: %+v", wakes)
	}
	decisions := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "triage.decision"}))
	if len(decisions) != 3 {
		t.Fatalf("agent-sourced events never enter triage: %d decisions", len(decisions))
	}
	if h, _, _ := connectors.TriageOnce(ctx, k); h != 3 {
		t.Fatalf("its own decisions are consumed as no-ops: %d", h)
	}
	if h, _, _ := connectors.TriageOnce(ctx, k); h != 0 {
		t.Fatal("cursor committed")
	}
}

func TestHTTPScheduleCapturesWithoutJudging(t *testing.T) {
	k := kerneltest.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Probe") != "1" {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(strings.Repeat("y", 5000)))
	}))
	defer srv.Close()
	name, action := "probe", "http"
	interval := 30
	sc := m(k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &interval,
		Spec: &kernel.ScheduleSpec{URL: srv.URL, Headers: map[string]string{"X-Probe": "1"}, Wake: true}}, "test"))
	status, err := connectors.FireSchedule(ctx, k, sc)
	if err != nil || status != "http 200" {
		t.Fatalf("%s %v", status, err)
	}
	cap := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.http"}))
	if len(cap) != 1 || len(cap[0].Payload.Str("body")) != 2000 || !cap[0].Payload.Bool("wake") {
		t.Fatalf("captured evidence, bounded: %+v", cap[0].Payload["status"])
	}
	fired := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "schedule.fired"}))
	if len(fired) != 1 || fired[0].Seq > cap[0].Seq {
		t.Fatal("intent before result")
	}
	after := m(k.GetSchedule(ctx, sc.ID))
	if after.LastStatus == nil || *after.LastStatus != "http 200" || after.LastRunAt == nil {
		t.Fatalf("bookkeeping: %+v", after)
	}
	srv.Close()
	status, _ = connectors.FireSchedule(ctx, k, after)
	if !strings.HasPrefix(status, "error:") {
		t.Fatalf("a dead endpoint is an error status, still captured: %s", status)
	}
}

func TestPromptScheduleSpeaksToThePrimary(t *testing.T) {
	k := kerneltest.New(t)
	name, action := "remind", "prompt"
	interval := 60
	sc := m(k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &interval, Spec: &kernel.ScheduleSpec{Prompt: "提醒我喝水"}}, "test"))
	status := m(connectors.FireSchedule(ctx, k, sc))
	if !strings.HasPrefix(status, "posted ev_") {
		t.Fatal(status)
	}
	msgs := m(k.PendingMessages(ctx, kernel.Primary, 10))
	if len(msgs) != 1 || msgs[0].Kind != "schedule.prompt" || msgs[0].Payload.Str("prompt") != "提醒我喝水" || msgs[0].Source != "connector:schedule:"+sc.ID {
		t.Fatalf("%+v", msgs)
	}
}

// The loop, on a clock the test moves: an interval schedule fires once per
// slot, its responses are recorded without waking a model, and a disabled
// schedule never fires.
func TestSchedulerFiresOncePerSlotUntilDisabled(t *testing.T) {
	k := kerneltest.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("up")) }))
	defer srv.Close()
	var mu sync.Mutex
	now := time.Now()
	k.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	name, action, interval := "health", "http", 15
	spec := &kernel.ScheduleSpec{URL: srv.URL}
	sc := m(k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &interval, Spec: spec}, "test"))
	off := m(k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &interval, Spec: spec}, "test"))
	disabled := false
	if off = m(k.UpdateSchedule(ctx, off.ID, kernel.ScheduleInput{Enabled: &disabled}, "test")); off.NextRunAt != nil {
		t.Fatalf("a disabled schedule has no next run: %s", *off.NextRunAt)
	}

	loop, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { connectors.Scheduler(loop, k, 5*time.Millisecond); close(done) }()
	defer func() { stop(); <-done }()
	fired := func(want int) []kernel.Event {
		t.Helper()
		var evs []kernel.Event
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if evs = m(k.ListEvents(ctx, kernel.ListFilter{Kind: "schedule.fired"})); len(evs) >= want {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // ten more ticks in the same slot
		if evs = m(k.ListEvents(ctx, kernel.ListFilter{Kind: "schedule.fired"})); len(evs) != want {
			t.Fatalf("fired %d times, want %d", len(evs), want)
		}
		return evs
	}
	fired(0)
	advance(15 * time.Second)
	fired(1)
	advance(15 * time.Second)
	for _, e := range fired(2) {
		if e.Payload.Str("scheduleId") != sc.ID {
			t.Fatalf("only the enabled schedule fires: %+v", e.Payload)
		}
	}
	captured := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.http"}))
	if len(captured) != 2 {
		t.Fatalf("each firing records the response: %+v", captured)
	}
	for _, c := range captured {
		if c.Payload["status"] != float64(200) || c.Payload.Str("body") != "up" {
			t.Fatalf("each firing records the response: %+v", c.Payload)
		}
	}
	if _, woke, err := connectors.TriageOnce(ctx, k); err != nil || woke != 0 {
		t.Fatalf("a recorded response wakes no model: woke=%d %v", woke, err)
	}
	ruled := map[string]string{}
	for _, d := range m(k.ListEvents(ctx, kernel.ListFilter{Kind: "triage.decision"})) {
		ruled[d.Payload.Str("of")] = d.Payload.Str("rule")
	}
	for _, c := range captured {
		if ruled[c.ID] != "scheduled-http-record-only" {
			t.Fatalf("every capture is triaged as record-only: %v", ruled)
		}
	}
}
