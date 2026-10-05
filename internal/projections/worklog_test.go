package projections_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/projections"
)

// A day's worklog is filed under worklogs/YYYY/MM/DD/worklog.md, each thread's
// events under their own heading.
func TestWorklogIsFiledByDay(t *testing.T) {
	k := kerneltest.New(t)
	item := m(k.CreateWorkItem(ctx, "报告", "test", kernel.CreateWorkItemOpts{}))
	say(k, "主线程说的话")
	m(k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "agent.reply", ThreadID: item.ThreadID, WorkItemID: item.ID,
		Payload: kernel.Payload{"text": "工作项里的回复"}}))
	day := k.Today()
	path := m(projections.WriteDay(ctx, k, day))
	if want := filepath.Join(k.Cfg.Home, "worklogs", day[0:4], day[5:7], day[8:10], "worklog.md"); path != want {
		t.Fatalf("filed at %s, want %s", path, want)
	}
	mainPart, itemPart, ok := strings.Cut(kernel.ReadTextFile(path), "## "+item.ID+" — 报告")
	if !ok || !strings.Contains(mainPart, "## Main thread") || !strings.Contains(mainPart, "主线程说的话") ||
		strings.Contains(mainPart, "工作项里的回复") || !strings.Contains(itemPart, "工作项里的回复") {
		t.Fatalf("each thread under its own heading:\n%s", kernel.ReadTextFile(path))
	}
}
