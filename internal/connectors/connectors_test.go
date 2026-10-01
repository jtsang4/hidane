package connectors_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if len(msgs) != 1 || msgs[0].Kind != "schedule.prompt" || msgs[0].Payload.Str("prompt") != "提醒我喝水" {
		t.Fatalf("%+v", msgs)
	}
}
