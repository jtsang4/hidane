package api_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// Hiding words said to a task masks the text but keeps where they went: the
// conversation still shows the message under its task.
func TestARedactedMessageKeepsItsTarget(t *testing.T) {
	e := newEnv(t, api.Options{})
	item := m(e.k.CreateWorkItem(t.Context(), "deploy", "test", kernel.CreateWorkItemOpts{}))
	_, body := e.do("POST", "/api/chat", "", map[string]any{"text": "密码是 hunter2", "target": item.ID})
	id, _ := body["messageId"].(string)
	if code, _ := e.do("POST", "/api/messages/"+id+"/redact", "", nil); code != 200 {
		t.Fatalf("redact: %d", code)
	}
	for _, path := range []string{"/api/events?page=1&conversation=1", "/api/work-items/" + item.ID} {
		_, page := e.do("GET", path, "", nil)
		var found map[string]any
		for _, ev := range eventsOf(page) {
			if ev["id"] == id {
				found = ev
			}
		}
		if found == nil {
			t.Fatalf("%s: the message stays listed", path)
		}
		p := found["payload"].(map[string]any)
		if p["redacted"] != true || p["text"] != "" || p["target"] != item.ID {
			t.Fatalf("%s: masked, still addressed to its task: %v", path, p)
		}
	}
}
