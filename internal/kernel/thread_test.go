package kernel_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
)

// Two work items never share a thread, even if a generated id collides.
func TestAThreadBelongsToOneWorkItem(t *testing.T) {
	k := kerneltest.New(t)
	a := m(k.CreateWorkItem(ctx, "a", "test", kernel.CreateWorkItemOpts{}))
	if _, err := k.DB.ExecContext(ctx, `INSERT INTO work_items (id, title, workspace, thread_id, created_at, updated_at)
		VALUES ('wi_dup', 'b', '/w', ?, '2026-01-01', '2026-01-01')`, a.ThreadID); err == nil {
		t.Fatal("a second work item on the same thread must be refused")
	}
}
