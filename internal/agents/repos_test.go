package agents_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/projections"
	"github.com/jtsang4/hidane/internal/repos"
	"github.com/jtsang4/hidane/internal/settings"
)

// hermeticGit keeps git away from the developer's own configuration.
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

// newRepo makes a git repository with one commit on main.
func newRepo(t *testing.T, parent, name, remote string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "init", "-q", "-b", "main")
	if remote != "" {
		run(t, dir, "git", "remote", "add", "origin", remote)
	}
	if files == nil {
		files = map[string]string{}
	}
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

type invocation struct {
	Kind string   `json:"kind"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
}

// workerCall is the first fake CLI run that had tools: the worker.
func workerCall(t *testing.T, log string) invocation {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var inv invocation
		if json.Unmarshal([]byte(line), &inv) != nil {
			continue
		}
		joined := strings.Join(inv.Args, " ")
		if strings.Contains(joined, "bypassPermissions") || strings.Contains(joined, "default_permissions") {
			return inv
		}
	}
	t.Fatalf("no worker run in %s", log)
	return invocation{}
}

func strconvQuote(s string) string { return strconv.Quote(s) }

func (w *world) onlyItem() kernel.WorkItem {
	w.t.Helper()
	items := m(w.k.ListWorkItems(ctx, ""))
	if len(items) != 1 {
		w.t.Fatalf("one work item: %+v", items)
	}
	return items[0]
}

func (w *world) lastReply(root string) string {
	w.t.Helper()
	text := ""
	for _, r := range w.events("agent.reply") {
		if r.Payload.Str("root") == root {
			text = r.Payload.Str("text")
		}
	}
	return text
}

// A new task gets its own worktree inside its workspace, set up by the repo's
// script before the first worker, and the worker works in it.
func TestNewTaskGetsItsOwnWorktree(t *testing.T) {
	for _, agent := range []string{settings.Claude, settings.Codex} {
		t.Run(agent, func(t *testing.T) {
			hermeticGit(t)
			repo := newRepo(t, t.TempDir(), "blog", "git@github.com:me/blog.git", map[string]string{
				"hidane.json": `{"worktree":{"setup":"echo ready > .setup-done","teardown":["echo bye > $HIDANE_SOURCE_CHECKOUT_PATH/torn-down"]}}`,
				".gitignore":  ".setup-done\n",
			})
			calls := filepath.Join(t.TempDir(), "calls.jsonl")
			w := newWorld(t, agent, "FAKEAGENT_LOG="+calls)
			msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "加一个 RSS 页面 REPO=" + repo, Source: "connector:web"}))
			w.settle()

			item := w.onlyItem()
			held := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID}))
			if len(held) != 1 {
				t.Fatalf("one checkout: %+v", held)
			}
			c := held[0]
			if c.Mode != kernel.CheckoutWorktree || c.Branch != "hidane/"+item.ID || c.Base != "main" ||
				c.Path != filepath.Join(item.Workspace, "blog") || c.Setup != kernel.SetupDone {
				t.Fatalf("a worktree of its own inside the workspace, set up: %+v", c)
			}
			if b, err := os.ReadFile(filepath.Join(c.Path, ".setup-done")); err != nil || !strings.Contains(string(b), "ready") {
				t.Fatalf("the setup script ran in the worktree: %q %v", b, err)
			}
			// Claude starts in the worktree, where the repo's own instructions
			// are found. Codex starts in the workspace and is handed them.
			start := c.Path
			if agent == settings.Codex {
				start = item.Workspace
			}
			if b, err := os.ReadFile(filepath.Join(start, "result.txt")); err != nil || !strings.Contains(string(b), "RSS") {
				t.Fatalf("the worker starts in %s: %q %v", start, b, err)
			}
			worker := workerCall(t, calls)
			if got, _ := filepath.EvalSymlinks(worker.Cwd); got != m(filepath.EvalSymlinks(start)) {
				t.Fatalf("worker cwd %s, want %s", worker.Cwd, start)
			}
			if agent == settings.Codex && !strings.Contains(strings.Join(worker.Args, " "), strconvQuote(item.Workspace)+`="write"`) {
				t.Fatalf("codex may write the workspace: %v", worker.Args)
			}
			if agent == settings.Codex && !strings.Contains(strings.Join(worker.Args, " "), strconvQuote(filepath.Join(repo, ".git"))+`="write"`) {
				t.Fatalf("codex may write the repo's git metadata: %v", worker.Args)
			}
			if _, err := os.Stat(filepath.Join(repo, "result.txt")); err == nil {
				t.Fatal("the person's own checkout is untouched")
			}
			if got := run(t, c.Path, "git", "rev-parse", "--abbrev-ref", "HEAD"); got != "hidane/"+item.ID {
				t.Fatalf("branch: %s", got)
			}
			var setupIntent bool
			for _, e := range w.events("side_effect.intent") {
				if e.Payload.Str("tool") == "setup" && e.ExecutionID != "" {
					setupIntent = true
				}
			}
			if !setupIntent {
				t.Fatal("setup is recorded as a side effect of the execution it ran for")
			}
			if reg := w.events("repo.registered"); len(reg) != 1 || reg[0].Payload.Str("name") != "blog" ||
				reg[0].Payload.Str("remote") != "github.com/me/blog" {
				t.Fatalf("a path the person gave is registered: %+v", reg)
			}
			if !strings.Contains(w.lastReply(msg.ID), "已完成") {
				t.Fatalf("reply: %q", w.lastReply(msg.ID))
			}

			// Archiving runs teardown, removes the directory and keeps the branch.
			if err := os.WriteFile(filepath.Join(c.Path, "draft.md"), []byte("wip"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.s.Repos.Archive(ctx, c.ID, false, "test"); err == nil {
				t.Fatal("uncommitted work is not thrown away without being asked")
			}
			if _, err := w.s.Repos.Archive(ctx, c.ID, true, "test"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(c.Path); !os.IsNotExist(err) {
				t.Fatalf("the worktree directory is removed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo, "torn-down")); err != nil {
				t.Fatal("teardown ran before removal")
			}
			var phases []string
			for _, e := range w.events("") {
				if (e.Kind == "side_effect.intent" || e.Kind == "side_effect.result") && e.Payload.Str("tool") == "teardown" {
					phases = append(phases, e.Kind)
				}
			}
			if strings.Join(phases, ",") != "side_effect.intent,side_effect.result" {
				t.Fatalf("teardown is a two-phase side effect: %v", phases)
			}
			if out := run(t, repo, "git", "branch", "--list", "hidane/"+item.ID); out == "" {
				t.Fatal("the branch is kept")
			}
			if got := m(w.k.GetCheckout(ctx, c.ID)); got.Status != kernel.CheckoutArchived {
				t.Fatalf("status: %s", got.Status)
			}
		})
	}
}

// Two tasks on the same repo run side by side, each on its own branch.
func TestParallelTasksOnOneRepoGetSeparateWorktrees(t *testing.T) {
	hermeticGit(t)
	repo := newRepo(t, t.TempDir(), "blog", "", nil)
	w := newWorld(t, settings.Claude)
	m(w.s.Repos.Register(ctx, repo, "test"))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "写 RSS REPO=blog", Source: "connector:web"}))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "首页提速 REPO=blog", Source: "connector:web"}))
	w.settle()
	held := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{Status: kernel.CheckoutActive}))
	if len(held) != 2 || held[0].Path == held[1].Path || held[0].Branch == held[1].Branch || held[0].WorkItemID == held[1].WorkItemID {
		t.Fatalf("two worktrees on two branches: %+v", held)
	}
	for _, c := range held {
		if _, err := os.Stat(filepath.Join(c.Path, "result.txt")); err != nil {
			t.Fatalf("each worker wrote into its own worktree: %s", c.Path)
		}
	}
}

// A name that fits two repos is asked about; nothing starts on a guess.
func TestAmbiguousRepoIsAskedBeforeAnythingStarts(t *testing.T) {
	hermeticGit(t)
	a := newRepo(t, t.TempDir(), "blog", "", nil)
	b := newRepo(t, t.TempDir(), "blog", "", nil)
	w := newWorld(t, settings.Claude)
	m(w.s.Repos.Register(ctx, a, "test"))
	m(w.s.Repos.Register(ctx, b, "test"))
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "加 RSS REPO=blog", Source: "connector:web"}))
	w.settle()
	if items := m(w.k.ListWorkItems(ctx, "")); len(items) != 0 {
		t.Fatalf("nothing is created before the person says which: %+v", items)
	}
	reply := w.lastReply(msg.ID)
	if !strings.Contains(reply, "对应多个仓库") || !strings.Contains(reply, a) || !strings.Contains(reply, b) {
		t.Fatalf("the question names both candidates: %q", reply)
	}
	// The suffixed name is unambiguous.
	msg2 := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "加 RSS REPO=blog-2", Source: "connector:web"}))
	w.settle()
	item := w.onlyItem()
	held := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID}))
	if len(held) != 1 || !strings.Contains(w.lastReply(msg2.ID), "已完成") {
		t.Fatalf("checkouts %+v reply %q", held, w.lastReply(msg2.ID))
	}
}

// A repo that moved is noticed, the person is asked, and pointing at its new
// place keeps its identity and repairs the worktrees that depend on it.
func TestMissingRepoIsNoticedAndCanBeRelocated(t *testing.T) {
	hermeticGit(t)
	parent := t.TempDir()
	repo := newRepo(t, parent, "blog", "https://github.com/me/blog.git", nil)
	w := newWorld(t, settings.Claude)
	r := m(w.s.Repos.Register(ctx, repo, "test"))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "写 RSS REPO=blog", Source: "connector:web"}))
	w.settle()
	item := w.onlyItem()
	moved := filepath.Join(parent, "blog-moved")
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "再加个暗色模式 REPO=blog", Source: "connector:web"}))
	w.settle()
	if reply := w.lastReply(msg.ID); !strings.Contains(reply, "不在 "+repo) {
		t.Fatalf("the person is told where it was expected: %q", reply)
	}
	if n := len(m(w.k.ListWorkItems(ctx, ""))); n != 1 {
		t.Fatalf("no work starts on a missing repo: %d items", n)
	}
	if ev := w.events("repo.missing"); len(ev) != 1 || ev[0].Payload.Str("repoId") != r.ID {
		t.Fatalf("the fact is recorded once: %+v", ev)
	}
	var notice bool
	for _, e := range w.events("escalation") {
		if e.Payload.Str("reason") == "repo_missing" && e.ThreadID == "main" && e.Payload.Str("repoId") == r.ID {
			notice = true
		}
	}
	if !notice {
		t.Fatal("the person is asked on the main thread")
	}
	// The same clone found at its new place keeps its id.
	got := m(w.s.Repos.Register(ctx, filepath.Join(parent, "blog-moved"), "test"))
	if got.ID != r.ID || got.Status != kernel.RepoPresent {
		t.Fatalf("a moved repo keeps its identity: %+v", got)
	}
	if ev := w.events("repo.relocated"); len(ev) != 1 {
		t.Fatalf("relocated: %+v", ev)
	}
	c := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID}))[0]
	if out := run(t, c.Path, "git", "status", "--porcelain"); strings.Contains(out, "fatal") {
		t.Fatalf("the worktree works again: %s", out)
	}
}

// The person's own directory is used only when asked, by one task at a time.
func TestInPlaceOnlyWhenAskedAndOneAtATime(t *testing.T) {
	hermeticGit(t)
	repo := newRepo(t, t.TempDir(), "notes", "", nil)
	w := newWorld(t, settings.Claude)
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "直接在主干上改 INPLACE REPO=" + repo, Source: "connector:web"}))
	w.settle()
	item := w.onlyItem()
	c := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID}))[0]
	if c.Mode != kernel.CheckoutInPlace || c.Path != repo || c.Branch != "main" {
		t.Fatalf("in place: %+v", c)
	}
	if _, err := os.Stat(filepath.Join(repo, "result.txt")); err != nil {
		t.Fatal("the worker wrote in the person's directory, which the guard allowed")
	}
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "也在主干上改 INPLACE REPO=notes", Source: "connector:web"}))
	w.settle()
	if reply := w.lastReply(msg.ID); !strings.Contains(reply, "正被任务") {
		t.Fatalf("the second writer is refused with a question: %q", reply)
	}
	m(w.s.Repos.Archive(ctx, c.ID, false, "test"))
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("releasing an in-place checkout never removes the person's directory")
	}
}

// Work picks up from an archived task's branch, and a finished task that still
// holds its worktree is reachable by a follow-up.
func TestContinueFromAnArchivedBranchAndRouteToAFinishedTask(t *testing.T) {
	hermeticGit(t)
	repo := newRepo(t, t.TempDir(), "blog", "", nil)
	w := newWorld(t, settings.Claude)
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "REPO=" + repo + " RUN: echo rss > rss.txt && git add rss.txt && git commit -qm rss", Source: "connector:web"}))
	w.settle()
	first := w.onlyItem()
	c := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: first.ID}))[0]
	if out := run(t, c.Path, "git", "log", "--oneline", "main..HEAD"); !strings.Contains(out, "rss") {
		t.Fatalf("the worker committed on its branch: %q", out)
	}

	// Finished, but its worktree still holds the work: a follow-up reaches it.
	m(w.s.ChangeStatus(ctx, first.ID, kernel.StatusDone, "test", nil))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "摘要也带上 ROUTE=" + first.ID, Source: "connector:web"}))
	w.settle()
	if got := m(w.k.GetWorkItem(ctx, first.ID)); got.Status != kernel.StatusOpen {
		t.Fatalf("a finished item with a worktree is routable and reopens: %s", got.Status)
	}
	var routed bool
	for _, e := range w.events("message.attributed") {
		if e.WorkItemID == first.ID && e.Payload.Str("by") == "model" && !e.Payload.Bool("created") {
			routed = true
		}
	}
	if !routed {
		t.Fatal("the follow-up was routed to the finished item")
	}

	// Archived: a new task continues from its branch.
	m(w.s.Repos.Archive(ctx, c.ID, true, "test"))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "接着 RSS 做 FROM=" + first.ID + " REPO=blog", Source: "connector:web"}))
	w.settle()
	var second kernel.WorkItem
	for _, it := range m(w.k.ListWorkItems(ctx, "")) {
		if it.ID != first.ID {
			second = it
		}
	}
	c2 := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: second.ID}))[0]
	if c2.Base != "hidane/"+first.ID || c2.Branch != "hidane/"+second.ID {
		t.Fatalf("continued from the archived branch: %+v", c2)
	}
	if _, err := os.Stat(filepath.Join(c2.Path, "rss.txt")); err != nil {
		t.Fatal("the new worktree has the earlier task's commit")
	}
	var shown string
	for _, card := range m(projections.BuildBoard(ctx, w.k, nil)) {
		if card.Item.ID == second.ID && len(card.Checkouts) == 1 {
			shown = card.Checkouts[0].Continues
		}
	}
	if shown != "hidane/"+first.ID {
		t.Fatalf("the card says which branch the work continues: %q", shown)
	}
}

// A fanned-out task's children each get a worktree branched from the
// parent's, and the parent hears where their work is.
func TestChildrenInheritTheParentsRepositories(t *testing.T) {
	hermeticGit(t)
	repo := newRepo(t, t.TempDir(), "site", "", nil)
	w := newWorld(t, settings.Claude)
	r := m(w.s.Repos.Register(ctx, repo, "test"))
	parent := m(w.k.CreateWorkItem(ctx, "parent", "test", kernel.CreateWorkItemOpts{}))
	pc := m(w.s.Repos.Attach(ctx, parent, r, repos.AttachSpec{}, "test"))
	m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "SPLIT_CHILDREN", Source: "connector:web", Target: parent.ID}))
	w.settle()
	children := m(w.k.ListChildren(ctx, parent.ID))
	if len(children) != 2 {
		t.Fatalf("children: %+v", children)
	}
	for _, ch := range children {
		held := m(w.k.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: ch.ID}))
		if len(held) != 1 || held[0].Base != pc.Branch || held[0].Branch != "hidane/"+ch.ID {
			t.Fatalf("a child's worktree starts from the parent's branch: %+v", held)
		}
	}
	settled := w.events("children.settled")
	if len(settled) != 1 {
		t.Fatalf("settled: %+v", settled)
	}
	list, _ := settled[0].Payload["children"].([]any)
	first, _ := list[0].(map[string]any)
	if branches, _ := first["branches"].([]any); len(branches) != 1 || !strings.Contains(branches[0].(string), "site: hidane/") {
		t.Fatalf("the parent is told each child's branch: %+v", first)
	}
}
