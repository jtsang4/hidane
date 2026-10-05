package api_test

import (
	"context"
	"os"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/repos/repostest"
)

// A worktree is not archived from under a task that is running in it, not
// even when the person forces past uncommitted work.
func TestARunningTasksWorktreeIsNotArchived(t *testing.T) {
	repostest.Hermetic(t)
	e := newEnv(t, api.Options{})
	if code, body := e.do("POST", "/api/repos", "", map[string]any{"path": repostest.New(t, "site", nil)}); code != 201 {
		t.Fatalf("register: %d %v", code, body)
	}
	_, body := e.do("POST", "/api/work-items", "", map[string]any{"title": "busy", "repo": "site"})
	id := body["item"].(map[string]any)["id"].(string)
	ctx := context.Background()
	if err := e.k.CreateExecution(ctx, "ex_busy", id, kernel.ManagerAddress(id)); err != nil {
		t.Fatal(err)
	}
	_, body = e.do("GET", "/api/checkouts", "", nil)
	co := body["checkouts"].([]any)[0].(map[string]any)
	if code, body := e.do("POST", "/api/checkouts/"+co["id"].(string)+"/archive", "", map[string]any{"force": true}); code != 409 || body["running"] != true {
		t.Fatalf("archived under a running task: %d %v", code, body)
	}
	if _, err := os.Stat(co["path"].(string)); err != nil {
		t.Fatal("the worktree is still there")
	}
}
