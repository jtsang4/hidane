package repos_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/repos"
	"github.com/jtsang4/hidane/internal/repos/repostest"
)

// files maps every file under dir, outside .git, to its content.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			out[rel] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Archiving a task that worked in the person's own directory only gives the
// directory back: no teardown, no removal — committed, changed, untracked
// and ignored files are all still there, as they were.
func TestArchivingAnInPlaceCheckoutDeletesNoFile(t *testing.T) {
	repostest.Hermetic(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := repostest.New(t, "notes", map[string]string{
		"hidane.json": `{"worktree":{"teardown":"rm -rf src README.md draft.txt .env"}}`,
		".gitignore":  ".env\n",
	})
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{"src/app.go": "package app\n", "README.md": "# notes, edited\n", "draft.txt": "untracked\n", ".env": "SECRET=1\n"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repostest.Git(t, dir, "add", "src")
	repostest.Git(t, dir, "commit", "-qm", "src")
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	item := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "in place", "test", kernel.CreateWorkItemOpts{}))
	c := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{InPlace: true}, "test"))
	if c.Mode != kernel.CheckoutInPlace || c.Path != dir {
		t.Fatalf("in place: %+v", c)
	}
	before := files(t, dir)
	status := repostest.Git(t, dir, "status", "--porcelain", "--ignored")

	archived := must[kernel.Checkout](t)(s.Archive(ctx, c.ID, true, "test"))
	if archived.Status != kernel.CheckoutArchived {
		t.Fatalf("archived: %+v", archived)
	}
	if after := files(t, dir); !maps.Equal(before, after) {
		t.Fatalf("archiving changed the person's files:\nbefore %v\nafter  %v", before, after)
	}
	if again := repostest.Git(t, dir, "status", "--porcelain", "--ignored"); again != status {
		t.Fatalf("git status changed:\n%s\n→\n%s", status, again)
	}
	events := must[[]kernel.Event](t)(k.ListEvents(ctx, kernel.ListFilter{Kind: "checkout.archived"}))
	if len(events) != 1 || !events[0].Payload.Bool("released") {
		t.Fatalf("recorded as given back: %+v", events)
	}
	for _, e := range must[[]kernel.Event](t)(k.ListEvents(ctx, kernel.ListFilter{Kind: "side_effect.intent"})) {
		if strings.Contains(e.Payload.Str("tool"), "teardown") {
			t.Fatalf("no teardown runs in the person's directory: %+v", e.Payload)
		}
	}
}
