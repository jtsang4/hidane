package api_test

import (
	"context"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// A refused request leaves nothing behind: a stop with nothing running, a
// memory with nothing in it.
func TestRefusedRequestsRecordNothing(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	item := m(e.k.CreateWorkItem(ctx, "idle", "test", kernel.CreateWorkItemOpts{}))
	before := m(e.k.LatestSeq(ctx))
	if code, _ := e.do("POST", "/api/work-items/"+item.ID+"/cancel", "", nil); code != 409 {
		t.Fatalf("nothing to stop: %d", code)
	}
	if code, _ := e.do("POST", "/api/memories", "", map[string]any{"kind": "preference", "content": "  "}); code != 400 {
		t.Fatalf("an empty memory: %d", code)
	}
	if after := m(e.k.LatestSeq(ctx)); after != before {
		t.Fatalf("%d events recorded for refused requests", after-before)
	}
	if entries := kernel.ParseMemories(kernel.ReadTextFile(e.k.GlobalMemoryPath())); len(entries) != 0 {
		t.Fatalf("written: %+v", entries)
	}
}
