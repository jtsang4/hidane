package api_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// A task's status changes through the API in both directions; a refused
// change, an unknown status or an unknown task, leaves no trace.
func TestWorkItemStatusGoesBothWays(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	item := m(e.k.CreateWorkItem(ctx, "x", "test", kernel.CreateWorkItemOpts{}))
	for _, status := range []string{kernel.StatusDone, kernel.StatusOpen} {
		if code, body := e.do("PATCH", "/api/work-items/"+item.ID, "", map[string]any{"status": status}); code != 200 || body["item"].(map[string]any)["status"] != status {
			t.Fatalf("%s: %d %v", status, code, body)
		}
	}
	if code, _ := e.do("PATCH", "/api/work-items/"+item.ID, "", map[string]any{"status": "banana"}); code != 400 {
		t.Fatalf("an unknown status: %d", code)
	}
	if code, _ := e.do("PATCH", "/api/work-items/wi_nope", "", map[string]any{"status": kernel.StatusDone}); code != 404 {
		t.Fatalf("an unknown task: %d", code)
	}
	changes := m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "work_item.status_changed"}))
	if len(changes) != 2 || changes[1].Payload.Str("from") != kernel.StatusDone || changes[1].Payload.Str("to") != kernel.StatusOpen {
		t.Fatalf("reopened, and nothing recorded for the refusals: %+v", changes)
	}
}

// A task can start without a conversation: a title alone opens it and wakes
// nobody, a brief is said to it like any message. Archiving hides it from the
// default list and deletes nothing.
func TestCreateAndArchiveAWorkItem(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	for _, title := range []string{"", "  "} {
		if code, _ := e.do("POST", "/api/work-items", "", map[string]any{"title": title}); code != 400 {
			t.Fatalf("title %q: %d", title, code)
		}
	}
	code, body := e.do("POST", "/api/work-items", "", map[string]any{"title": "later"})
	if quiet := body["item"].(map[string]any); code != 201 || body["dispatched"] != false || quiet["status"] != kernel.StatusOpen ||
		len(m(e.k.PendingMessages(ctx, kernel.ManagerAddress(quiet["id"].(string)), 10))) != 0 {
		t.Fatalf("a title alone dispatches nothing: %d %v", code, body)
	}
	_, body = e.do("POST", "/api/work-items", "", map[string]any{"title": "now", "brief": "write the notes"})
	item := body["item"].(map[string]any)
	id := item["id"].(string)
	said := m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "user.message", ThreadID: "main"}))
	if len(said) != 1 || said[0].Payload.Str("text") != "write the notes" || said[0].Payload.Str("target") != id {
		t.Fatalf("the brief is said to the task on the main thread: %+v", said)
	}
	if att := m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "message.attributed"})); len(att) != 1 || att[0].WorkItemID != id || att[0].Payload.Str("by") != "explicit" {
		t.Fatalf("attributed: %+v", att)
	}
	if got := m(e.k.PendingMessages(ctx, kernel.ManagerAddress(id), 10)); len(got) != 1 || got[0].Payload.Str("text") != "write the notes" {
		t.Fatalf("its Manager has the brief: %+v", got)
	}

	product := filepath.Join(item["workspace"].(string), "notes.md")
	if err := os.WriteFile(product, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do("PATCH", "/api/work-items/"+id, "", map[string]any{"status": kernel.StatusClosed}); code != 200 {
		t.Fatalf("archive: %d", code)
	}
	listed := func(query string) map[string]any {
		_, body := e.do("GET", "/api/work-items"+query, "", nil)
		for _, it := range body["items"].([]any) {
			if it.(map[string]any)["id"] == id {
				return it.(map[string]any)
			}
		}
		return nil
	}
	if listed("") != nil || listed("?all") == nil || listed("?all")["status"] != kernel.StatusClosed {
		t.Fatal("an archived task leaves the default list, not ?all")
	}
	if _, err := os.Stat(product); err != nil || len(m(e.k.ListEvents(ctx, kernel.ListFilter{WorkItemID: id}))) == 0 {
		t.Fatal("archiving deletes nothing")
	}
}
