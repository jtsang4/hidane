package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/settings"
)

// A task that takes minutes does not hold up the conversation: a question
// asked while its worker runs is answered at once under its own root, and the
// task's result still answers the message that started it.
func TestTheConversationGoesOnWhileATaskRuns(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude, "FAKEAGENT_WORKER_DELAY_MS=8000")
	task := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "创建一个文件写上 slow", Source: "connector:web"}))
	if err := w.rt.Drain(50); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); w.s.Pool.Status()["running"] != 1; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the worker never started")
		}
	}
	small := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好，顺便问一句", Source: "connector:web"}))
	if err := w.rt.Drain(50); err != nil {
		t.Fatal(err)
	}
	if w.lastReply(small.ID) == "" {
		t.Fatal("the question is answered under its own root")
	}
	srv := httptest.NewServer(api.New(api.Options{K: w.k, Sys: w.s, Settings: w.s.Settings,
		Detect: func(context.Context) []agentcli.Detection { return nil }}))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Runtime struct {
			PendingMessages int            `json:"pendingMessages"`
			Workers         map[string]int `json:"workers"`
		} `json:"runtime"`
	}
	_ = json.NewDecoder(res.Body).Decode(&status)
	res.Body.Close()
	if status.Runtime.Workers["running"] != 1 || status.Runtime.PendingMessages != 0 {
		t.Fatalf("answered while the worker still runs, nothing left waiting: %+v", status.Runtime)
	}
	w.settle()
	if reply := w.lastReply(task.ID); !strings.Contains(reply, "已完成") {
		t.Fatalf("the result answers the message that started the task: %q", reply)
	}
}
