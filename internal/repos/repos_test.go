package repos_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/repos"
)

var ctx = context.Background()

func hermeticGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "GIT_CONFIG_GLOBAL": filepath.Join(home, "gitconfig"), "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "init", "-q", "-b", "main")
	files["README.md"] = "# " + name + "\n"
	for p, c := range files {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "init")
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:Me/Blog.git":          "github.com/me/blog",
		"https://github.com/me/blog":          "github.com/me/blog",
		"https://user@github.com/me/blog.git": "github.com/me/blog",
		"ssh://git@github.com/me/blog.git/":   "github.com/me/blog",
		"":                                    "",
	} {
		if got := repos.NormalizeRemote(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// The person's own file wins, then the repo's hidane.json, then a paseo.json
// the repo already has — field by field.
func TestConfigPrecedence(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "app", map[string]string{
		"hidane.json": `{"worktree":{"setup":["pnpm install","cp $HIDANE_SOURCE_CHECKOUT_PATH/.env .env"]}}`,
		"paseo.json":  `{"worktree":{"setup":"npm ci","teardown":"rm -rf .cache"}}`,
	})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	cfg := s.Config(r, dir)
	if strings.Join(cfg.Setup, "|") != "pnpm install|cp $HIDANE_SOURCE_CHECKOUT_PATH/.env .env" || strings.Join(cfg.Teardown, "|") != "rm -rf .cache" {
		t.Fatalf("hidane.json setup, paseo.json teardown: %+v", cfg)
	}
	if err := os.MkdirAll(filepath.Dir(s.ConfigFile(r)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ConfigFile(r), []byte(`{"worktree":{"setup":""}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg = s.Config(r, dir)
	if len(cfg.Setup) != 0 || len(cfg.Teardown) != 1 {
		t.Fatalf("an empty personal setup switches the repo's off: %+v", cfg)
	}
}

func TestRegisterAndResolve(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "blog", map[string]string{})
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := must[kernel.Repo](t)(s.Register(ctx, filepath.Join(dir, "src"), "test"))
	if r.Path != dir || r.Name != "blog" || r.DefaultBranch != "main" {
		t.Fatalf("a subdirectory registers its repository: %+v", r)
	}
	again := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	if again.ID != r.ID {
		t.Fatal("the same path is the same repo")
	}
	if got := must[kernel.Repo](t)(s.Resolve(ctx, "BLOG", "test")); got.ID != r.ID {
		t.Fatal("names resolve case-insensitively")
	}
	if _, err := s.Resolve(ctx, "nope", "test"); !errors.Is(err, repos.ErrUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	plain := t.TempDir()
	if _, err := s.Register(ctx, plain, "test"); !errors.Is(err, repos.ErrNotGit) {
		t.Fatalf("not git: %v", err)
	}
	if _, err := s.Register(ctx, filepath.Join(plain, "nope"), "test"); !errors.Is(err, repos.ErrNoSuchDir) {
		t.Fatalf("no dir: %v", err)
	}
	inside := filepath.Join(k.Cfg.Home, "x")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, inside, "git", "init", "-q")
	if _, err := s.Register(ctx, inside, "test"); !errors.Is(err, repos.ErrInsideHome) {
		t.Fatalf("hidane's own data directory is never a repo to work on: %v", err)
	}
}

// Archive refuses to drop uncommitted work, then removes the directory and
// keeps the branch; Describe reports what is on it.
func TestDescribeAndArchive(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "blog", map[string]string{})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	item := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "rss", "test", kernel.CreateWorkItemOpts{}))
	c := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test"))
	if again := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test")); again.ID != c.ID {
		t.Fatal("attaching twice returns the same checkout")
	}
	if err := os.WriteFile(filepath.Join(c.Path, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, c.Path, "git", "add", "a.txt")
	run(t, c.Path, "git", "commit", "-qm", "a")
	if err := os.WriteFile(filepath.Join(c.Path, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := s.Describe(ctx, c, r)
	if st.Health != "ok" || st.Ahead != 1 || st.Dirty != 1 || st.Head != c.Branch {
		t.Fatalf("describe: %+v", st)
	}
	var dirty *repos.DirtyError
	if _, err := s.Archive(ctx, c.ID, false, "test"); !errors.As(err, &dirty) || dirty.Files != 1 {
		t.Fatalf("dirty: %v", err)
	}
	archived := must[kernel.Checkout](t)(s.Archive(ctx, c.ID, true, "test"))
	if archived.Status != kernel.CheckoutArchived {
		t.Fatalf("status: %+v", archived)
	}
	if _, err := os.Stat(c.Path); !os.IsNotExist(err) {
		t.Fatal("the directory is gone")
	}
	if run(t, dir, "git", "branch", "--list", c.Branch) == "" {
		t.Fatal("the branch stays")
	}
	if out := run(t, dir, "git", "worktree", "list"); strings.Contains(out, c.Path) {
		t.Fatalf("git forgot the worktree: %s", out)
	}
	// Attached again, the item carries on from its own branch.
	back := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test"))
	if _, err := os.Stat(filepath.Join(back.Path, "a.txt")); err != nil || back.Base != c.Branch {
		t.Fatalf("re-attached on the kept branch: %+v %v", back, err)
	}
}

// A repo that disappears is recorded once and the person is asked once.
func TestCheckAllNoticesAMissingRepoOnce(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "blog", map[string]string{})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		must[[]kernel.Repo](t)(s.CheckAll(ctx, "test"))
	}
	missing := must[[]kernel.Event](t)(k.ListEvents(ctx, kernel.ListFilter{Kind: "repo.missing"}))
	asks := must[[]kernel.Event](t)(k.ListEvents(ctx, kernel.ListFilter{Kind: "escalation"}))
	if len(missing) != 1 || len(asks) != 1 || asks[0].Payload.Str("reason") != "repo_missing" || asks[0].Payload.Str("root") == "" {
		t.Fatalf("missing %+v asks %+v", missing, asks)
	}
	var amb *repos.MissingError
	if _, err := s.Resolve(ctx, "blog", "test"); !errors.As(err, &amb) || amb.Repo.ID != r.ID {
		t.Fatalf("resolving a missing repo says so: %v", err)
	}
	if err := s.Forget(ctx, r.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if list := must[[]kernel.Repo](t)(k.ListRepos(ctx)); len(list) != 0 {
		t.Fatalf("forgotten: %+v", list)
	}
}

// A failing setup is recorded and reported, not fatal.
func TestRunSetupRecordsFailure(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "blog", map[string]string{"hidane.json": `{"worktree":{"setup":"echo installing\nexit 3\necho never"}}`})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	item := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "rss", "test", kernel.CreateWorkItemOpts{}))
	c := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test"))
	if c.Setup != kernel.SetupPending {
		t.Fatalf("setup pending: %+v", c)
	}
	res := s.RunSetup(ctx, item, c, "ex_test")
	if !res.Ran || res.OK || !strings.Contains(res.Tail, "installing") || strings.Contains(res.Tail, "\nnever") {
		t.Fatalf("a script stops at its first failing line: %+v", res)
	}
	if got := must[kernel.Checkout](t)(k.GetCheckout(ctx, c.ID)); got.Setup != kernel.SetupFailed {
		t.Fatalf("setup state: %s", got.Setup)
	}
	results := must[[]kernel.Event](t)(k.ListEvents(ctx, kernel.ListFilter{Kind: "side_effect.result"}))
	if len(results) != 1 || !results[0].Payload.Bool("isError") || results[0].ExecutionID != "ex_test" {
		t.Fatalf("the result is recorded against the execution: %+v", results)
	}
}

// A worktree directory deleted by hand does not stop the item from getting
// its branch back, and a setup cut short by a restart runs again.
func TestRecoveringFromHandDeletionAndInterruptedSetup(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "blog", map[string]string{"hidane.json": `{"worktree":{"setup":"true"}}`})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	item := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "rss", "test", kernel.CreateWorkItemOpts{}))
	c := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test"))
	if err := k.SetCheckoutSetup(ctx, c.ID, kernel.SetupRunning); err != nil {
		t.Fatal(err)
	}
	if n := must[int64](t)(k.ResumeInterruptedSetups(ctx)); n != 1 {
		t.Fatalf("resumed %d", n)
	}
	if got := must[kernel.Checkout](t)(k.GetCheckout(ctx, c.ID)); got.Setup != kernel.SetupPending {
		t.Fatalf("setup: %s", got.Setup)
	}
	if err := os.RemoveAll(c.Path); err != nil {
		t.Fatal(err)
	}
	must[kernel.Checkout](t)(k.ArchiveCheckout(ctx, c.ID, "test", nil))
	back := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test"))
	if back.Branch != c.Branch || !repos.Present(back.Path) {
		t.Fatalf("re-attached: %+v", back)
	}
}

// Forgetting or archiving when the repository is gone deletes nothing: git can
// no longer say what in the worktree is unsaved.
func TestNothingIsDeletedWhenTheRepoIsGone(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "ledger", map[string]string{})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	a := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "a", "test", kernel.CreateWorkItemOpts{}))
	b := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "b", "test", kernel.CreateWorkItemOpts{}))
	ca := must[kernel.Checkout](t)(s.Attach(ctx, a, r, repos.AttachSpec{}, "test"))
	cb := must[kernel.Checkout](t)(s.Attach(ctx, b, r, repos.AttachSpec{}, "test"))
	for _, c := range []kernel.Checkout{ca, cb} {
		if err := os.WriteFile(filepath.Join(c.Path, "wip.txt"), []byte("unsaved"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal(err)
	}
	must[[]kernel.Repo](t)(s.CheckAll(ctx, "test"))

	got := must[kernel.Checkout](t)(s.Archive(ctx, ca.ID, true, "test"))
	if got.Status != kernel.CheckoutArchived {
		t.Fatalf("archived: %+v", got)
	}
	if err := s.Forget(ctx, r.ID, "test"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []kernel.Checkout{ca, cb} {
		if b, err := os.ReadFile(filepath.Join(c.Path, "wip.txt")); err != nil || string(b) != "unsaved" {
			t.Fatalf("%s: the person's unsaved work is still there: %q %v", c.Path, b, err)
		}
	}
	archived := must[[]kernel.Event](t)(k.ListEvents(ctx, kernel.ListFilter{Kind: "checkout.archived"}))
	if len(archived) != 2 {
		t.Fatalf("both recorded as archived: %+v", archived)
	}
	for _, e := range archived {
		if e.Payload.Str("directoryKept") == "" {
			t.Fatalf("the record says the directory was left in place: %+v", e.Payload)
		}
	}
}

// "Ahead" is measured against the trunk, so a branch continued from another
// task's branch counts that work too.
func TestAheadIsCountedAgainstTheTrunk(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "blog", map[string]string{})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	first := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "first", "test", kernel.CreateWorkItemOpts{}))
	c1 := must[kernel.Checkout](t)(s.Attach(ctx, first, r, repos.AttachSpec{}, "test"))
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(c1.Path, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, c1.Path, "git", "add", name)
		run(t, c1.Path, "git", "commit", "-qm", name)
	}
	must[kernel.Checkout](t)(s.Archive(ctx, c1.ID, false, "test"))
	second := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "second", "test", kernel.CreateWorkItemOpts{}))
	c2 := must[kernel.Checkout](t)(s.Attach(ctx, second, r, repos.AttachSpec{From: first.ID}, "test"))
	if err := os.WriteFile(filepath.Join(c2.Path, "c.txt"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, c2.Path, "git", "add", "c.txt")
	run(t, c2.Path, "git", "commit", "-qm", "c")
	if st := s.Describe(ctx, c2, must[kernel.Repo](t)(k.GetRepo(ctx, r.ID))); st.Ahead != 3 || st.Base != c1.Branch {
		t.Fatalf("three commits the trunk lacks: %+v", st)
	}
}

// A task's changes are everything since its branch left the trunk: commits,
// uncommitted edits and new files, each with its line counts.
func TestChangesSinceTheBranchLeftTheTrunk(t *testing.T) {
	hermeticGit(t)
	k := kerneltest.New(t)
	s := repos.New(k)
	dir := newRepo(t, "site", map[string]string{"old.txt": "one\ntwo\n"})
	r := must[kernel.Repo](t)(s.Register(ctx, dir, "test"))
	item := must[kernel.WorkItem](t)(k.CreateWorkItem(ctx, "feed", "test", kernel.CreateWorkItemOpts{}))
	c := must[kernel.Checkout](t)(s.Attach(ctx, item, r, repos.AttachSpec{}, "test"))
	// The trunk moves on after the branch left it; that is not the task's change.
	if err := os.WriteFile(filepath.Join(dir, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "later")
	if err := os.WriteFile(filepath.Join(c.Path, "feed.xml"), []byte("<rss/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, c.Path, "git", "add", "feed.xml")
	run(t, c.Path, "git", "commit", "-qm", "add feed")
	if err := os.WriteFile(filepath.Join(c.Path, "old.txt"), []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "notes.md"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := s.Changes(ctx, c, r)
	if ch.Problem != "" || ch.Against != "main" || len(ch.Commits) != 1 || ch.Commits[0].Subject != "add feed" {
		t.Fatalf("changes: %+v", ch)
	}
	got := map[string]repos.FileChange{}
	for _, f := range ch.Files {
		got[f.Path] = f
	}
	if f := got["feed.xml"]; f.Status != "added" || f.Additions != 1 {
		t.Fatalf("committed file: %+v", got)
	}
	if f := got["old.txt"]; f.Status != "modified" || f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("uncommitted edit: %+v", got)
	}
	if f := got["notes.md"]; f.Status != "untracked" || f.Additions != 2 {
		t.Fatalf("new file: %+v", got)
	}
	if _, ok := got["later.txt"]; ok {
		t.Fatal("the trunk's own later work is not the task's")
	}
	for _, want := range []string{"+<rss/>", "+TWO", "+three", "b/notes.md", "+b"} {
		if !strings.Contains(ch.Patch, want) {
			t.Fatalf("patch lacks %q:\n%s", want, ch.Patch)
		}
	}
	if err := os.RemoveAll(c.Path); err != nil {
		t.Fatal(err)
	}
	if gone := s.Changes(ctx, c, r); gone.Problem != "missing" {
		t.Fatalf("a missing directory says so: %+v", gone)
	}
}
