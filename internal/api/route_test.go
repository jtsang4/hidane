package api_test

import (
	"context"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// Moving a message to a new task is the person creating it: the task runs on
// what the message chose, and the attribution says it was created.
func TestRouteToANewTaskCreatesItFromTheMessage(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	runAs := kernel.RunAs{Agent: "codex", Model: "gpt-fake-1", Effort: "high"}
	_, body := e.do("POST", "/api/chat", "", map[string]any{"text": "  整理\n  周报  ",
		"runAs": map[string]any{"agent": runAs.Agent, "model": runAs.Model, "effort": runAs.Effort}})
	msg, _ := body["messageId"].(string)
	// The second move leaves the first new task as the previous one.
	previous := ""
	for range 2 {
		code, res := e.do("POST", "/api/messages/"+msg+"/route", "", map[string]any{"workItemId": "new"})
		id, _ := res["workItemId"].(string)
		if code != 200 || id == "" {
			t.Fatalf("route to new: %d %v", code, res)
		}
		item := m(e.k.GetWorkItem(ctx, id))
		if item.Title != "整理 周报" || item.RunAs == nil || *item.RunAs != runAs {
			t.Fatalf("the new task: %q %+v", item.Title, item.RunAs)
		}
		got := m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "message.attributed", PayloadKey: "of", PayloadValue: msg}))
		if len(got) == 0 {
			t.Fatal("no message.attributed")
		}
		a := got[len(got)-1]
		if a.WorkItemID != id || a.Payload.Str("by") != "user" || !a.Payload.Bool("created") || a.Payload.Str("previous") != previous {
			t.Fatalf("attributed: %+v", a.Payload)
		}
		previous = id
	}
	if code, _ := e.do("POST", "/api/messages/ev_nope/route", "", map[string]any{"workItemId": "new"}); code != 404 {
		t.Fatal("unknown message")
	}
}
