// Package connectors capture and normalize events from the outside world.
// They never judge, never call agents directly, and never block on processing.
package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/kernel"
)

// Heartbeat proves the observe path stays alive; triage keeps it record-only.
func Heartbeat(ctx context.Context, k *kernel.Kernel, every time.Duration) {
	emit := func() {
		if _, err := k.Append(ctx, kernel.EventInput{Source: "connector:timer", Kind: "connector.heartbeat",
			Payload: kernel.Payload{"at": kernel.FormatTime(k.Now())}}); err != nil && ctx.Err() == nil {
			log.Printf("heartbeat append failed: %v", err)
		}
	}
	emit()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			emit()
		}
	}
}

const triageConsumer = "triage"

// TriageOnce consumes connector events off the log with a cursor and runs the
// deterministic rules. A wake is a message posted to the Primary's mailbox —
// the decision record doubles as the delivery — so triage never waits for it.
func TriageOnce(ctx context.Context, k *kernel.Kernel) (handled, woke int, err error) {
	batch, err := k.NextBatch(ctx, triageConsumer, 50)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range batch {
		if kernel.NeedsTriage(e) {
			rule, action := kernel.TriageEvent(e, kernel.DefaultTriageRules)
			payload := kernel.Payload{"of": e.ID, "ofKind": e.Kind, "rule": rule, "action": action}
			if action == "wake_primary" {
				woke++
				b, _ := json.Marshal(e.Payload)
				body := string(b)
				if len(body) > 1500 {
					body = body[:1500]
				}
				payload["summary"] = fmt.Sprintf("External event %s from %s: %s", e.Kind, e.Source, body)
				ev := e
				if _, _, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "kernel:triage", Kind: "triage.decision",
					Mailbox: kernel.Primary, Lane: kernel.LaneNormal, Payload: payload}, CausedBy: &ev}); err != nil {
					return handled, woke, err
				}
			} else if _, err := k.Append(ctx, kernel.EventInput{Source: "kernel:triage", Kind: "triage.decision", Payload: payload}); err != nil {
				return handled, woke, err
			}
		}
		if err := k.CommitCursor(ctx, triageConsumer, e.Seq); err != nil {
			return handled, woke, err
		}
		handled++
	}
	return handled, woke, nil
}

// TriageLoop runs triage on every append and on a fallback poll. Its own
// decisions wake it too; the cursor makes those wake-ups cheap no-ops.
func TriageLoop(ctx context.Context, k *kernel.Kernel, every time.Duration) {
	k.Consume(ctx, "triage loop", every, 50, func(ctx context.Context) (int, error) {
		n, _, err := TriageOnce(ctx, k)
		return n, err
	})
}

const (
	httpTimeout  = 30 * time.Second
	captureLimit = 2000
)

// Captured response bodies are evidence, not archives — the log is not a cache.
func fireHTTP(ctx context.Context, k *kernel.Kernel, sc kernel.Schedule) string {
	started := time.Now()
	method := sc.Spec.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if sc.Spec.Body != "" && method != http.MethodGet {
		body = strings.NewReader(sc.Spec.Body)
	}
	payload := kernel.Payload{"scheduleId": sc.ID, "name": sc.Name, "url": sc.Spec.URL, "wake": sc.Spec.Wake}
	status := ""
	reqCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, sc.Spec.URL, body)
	if err == nil {
		for k, v := range sc.Spec.Headers {
			req.Header.Set(k, v)
		}
		var res *http.Response
		res, err = http.DefaultClient.Do(req)
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(res.Body, captureLimit))
			res.Body.Close()
			payload["status"], payload["ok"], payload["body"] = res.StatusCode, res.StatusCode < 400, string(b)
			status = fmt.Sprintf("http %d", res.StatusCode)
		}
	}
	if err != nil {
		payload["status"], payload["ok"], payload["error"] = 0, false, err.Error()
		status = "error: " + err.Error()
	}
	payload["durationMs"] = time.Since(started).Milliseconds()
	if _, err := k.Append(ctx, kernel.EventInput{Source: "connector:schedule", Kind: "connector.http", Payload: payload}); err != nil {
		return "error: " + err.Error()
	}
	return status
}

// firePrompt speaks to the Primary on the person's behalf. Posting is the
// whole firing: the answer arrives later, so a long chain never holds up the
// next due schedule.
func firePrompt(ctx context.Context, k *kernel.Kernel, sc kernel.Schedule, fired kernel.Event) (string, error) {
	ev, ok, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "connector:schedule:" + sc.ID, Kind: "schedule.prompt",
		Mailbox: kernel.Primary, Lane: kernel.LaneNormal,
		Payload: kernel.Payload{"scheduleId": sc.ID, "name": sc.Name, "prompt": sc.Spec.Prompt}}, CausedBy: &fired})
	if err != nil {
		return "", err
	}
	if !ok {
		return "budget exceeded", nil
	}
	return "posted " + ev.ID, nil
}

// FireSchedule fires one schedule now (loop tick or run-now). Two-phase like
// every side effect: schedule.fired first, then the captured result.
func FireSchedule(ctx context.Context, k *kernel.Kernel, sc kernel.Schedule) (string, error) {
	fired, err := k.Append(ctx, kernel.EventInput{Source: "connector:schedule", Kind: "schedule.fired",
		Payload: kernel.Payload{"scheduleId": sc.ID, "name": sc.Name, "action": sc.Action}})
	if err != nil {
		return "", err
	}
	var status string
	if sc.Action == "http" {
		status = fireHTTP(ctx, k, sc)
	} else {
		status, err = firePrompt(ctx, k, sc, fired)
		if err != nil {
			status = "error: " + err.Error()
			_, _ = k.Append(ctx, kernel.EventInput{Source: "connector:schedule", Kind: "agent.error",
				Payload: kernel.Payload{"scheduleId": sc.ID, "error": status}})
		}
	}
	return status, k.MarkRun(ctx, sc.ID, status, k.Now())
}

// Scheduler fires due schedules; a slow tick never stacks.
func Scheduler(ctx context.Context, k *kernel.Kernel, every time.Duration) {
	tick := func() {
		due, err := k.DueSchedules(ctx, k.Now())
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("scheduler tick failed: %v", err)
			}
			return
		}
		for _, sc := range due {
			if _, err := FireSchedule(ctx, k, sc); err != nil && ctx.Err() == nil {
				log.Printf("schedule %s failed: %v", sc.ID, err)
			}
		}
	}
	tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
