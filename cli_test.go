package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/kernel"
)

// The CLI's memory and run-as switches: a work item's memory is listed with
// its id and forgotten by it, an unknown id is an error; --agent pins the task
// the message starts, and --model or --effort without it is an error.
func TestCLIMemoriesAndRunAs(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	home := fakeHome(t)
	ctx := context.Background()
	k, err := kernel.Open(config.ForTest(home))
	if err != nil {
		t.Fatal(err)
	}
	item, err := k.CreateWorkItem(ctx, "site", "test", kernel.CreateWorkItemOpts{})
	if err != nil {
		t.Fatal(err)
	}
	entry, _, err := k.PromoteToFile(ctx, "decision", "deploy from main", "work_item", item.Workspace, item.ID, "test")
	k.Close()
	if err != nil {
		t.Fatal(err)
	}
	if out := run(t, bin, home, "memories", "--ids"); !strings.Contains(out, entry.ID) || !strings.Contains(out, "deploy from main") {
		t.Fatalf("memories --ids:\n%s", out)
	}
	run(t, bin, home, "forget", entry.ID)
	if out := run(t, bin, home, "memories", "--ids"); strings.Contains(out, entry.ID) {
		t.Fatalf("forgotten:\n%s", out)
	}
	fails := func(args ...string) string {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "HIDANE_HOME="+home, "HIDANE_LOGIN_SHELL=0")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("hidane %v must fail:\n%s", args, out)
		}
		return string(out)
	}
	fails("forget", entry.ID)
	if out := fails("chat", "--effort", "high", "写一个文件"); !strings.Contains(out, "need --agent") {
		t.Fatalf("--effort alone:\n%s", out)
	}
	if out := run(t, bin, home, "chat", "--timeout", "60", "--agent", "codex", "--effort", "high", "写一个文件，内容是 pinned"); !strings.Contains(out, "已完成") {
		t.Fatalf("chat --agent:\n%s", out)
	}
	k, err = kernel.Open(config.ForTest(home))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	items, _ := k.ListWorkItems(ctx, "")
	for _, it := range items {
		if it.ID != item.ID && (it.RunAs == nil || it.RunAs.Agent != "codex" || it.RunAs.Effort != "high") {
			t.Fatalf("the task runs as chosen: %+v", it.RunAs)
		}
	}
	if len(items) != 2 {
		t.Fatalf("items: %+v", items)
	}
}
