// Package repos materializes a work item's access to the person's
// repositories: registering them, git worktrees inside the work item's
// workspace, the setup and teardown scripts a repo asks for, and noticing
// when a repo is gone. The kernel keeps the state and the facts; this package
// is the only one that knows they are git.
package repos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/clip"
	"github.com/jtsang4/hidane/internal/kernel"
)

type Service struct {
	K *kernel.Kernel
	// Env is the environment git and the scripts run with (the login shell's
	// PATH for a Finder-launched app); nil means this process's.
	Env []string
	// SetupTimeout bounds each setup or teardown script.
	SetupTimeout time.Duration

	// mu serializes changes to worktrees: git takes a lock per repository,
	// and two turns may attach the same repo at once.
	mu sync.Mutex
}

func New(k *kernel.Kernel) *Service {
	timeout := k.Cfg.SetupTimeout
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	return &Service{K: k, SetupTimeout: timeout}
}

var (
	ErrNotGit     = errors.New("not a git repository")
	ErrNoSuchDir  = errors.New("no such directory")
	ErrInsideHome = errors.New("the directory is inside hidane's data directory")
	ErrUnknown    = errors.New("no registered repository by that name")
)

// AmbiguousError: a name that fits more than one registered repo.
type AmbiguousError struct {
	Ref        string
	Candidates []kernel.Repo
}

func (e *AmbiguousError) Error() string {
	var names []string
	for _, c := range e.Candidates {
		names = append(names, c.Name+" ("+c.Path+")")
	}
	return fmt.Sprintf("%q matches several repositories: %s", e.Ref, strings.Join(names, ", "))
}

// MissingError: the repo is registered but no longer where it was.
type MissingError struct{ Repo kernel.Repo }

func (e *MissingError) Error() string {
	return fmt.Sprintf("repository %s is no longer at %s", e.Repo.Name, e.Repo.Path)
}

// DirtyError: archiving would throw away uncommitted changes.
type DirtyError struct{ Files int }

func (e *DirtyError) Error() string {
	return fmt.Sprintf("the checkout has %d uncommitted change(s)", e.Files)
}

func (s *Service) environ() []string {
	env := s.Env
	if env == nil {
		env = os.Environ()
	}
	// git must never stop to ask a person at a terminal nobody watches.
	return append(append([]string{}, env...), "GIT_TERMINAL_PROMPT=0")
}

func (s *Service) git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = s.environ()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], clip.Runes(msg, 400))
	}
	return strings.TrimSpace(out.String()), nil
}

// ExpandPath turns what a person typed into an absolute, clean path.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// LooksLikePath: a reference that names a directory rather than a repo.
func LooksLikePath(ref string) bool {
	ref = strings.TrimSpace(ref)
	return strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") || strings.HasPrefix(ref, ".") ||
		filepath.IsAbs(ref) || strings.ContainsRune(ref, filepath.Separator) || strings.Contains(ref, "/")
}

var scpRemote = regexp.MustCompile(`^[\w.-]+@([\w.-]+):(.+)$`)

// NormalizeRemote makes the clone URLs of one repository compare equal:
// git@github.com:a/b.git, https://github.com/a/b and ssh://git@github.com/a/b.
func NormalizeRemote(url string) string {
	u := strings.TrimSpace(url)
	if u == "" {
		return ""
	}
	if m := scpRemote.FindStringSubmatch(u); m != nil {
		u = m[1] + "/" + m[2]
	} else {
		if i := strings.Index(u, "://"); i >= 0 {
			u = u[i+3:]
		}
		if i := strings.Index(u, "@"); i >= 0 && i < strings.Index(u+"/", "/") {
			u = u[i+1:]
		}
	}
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	return strings.ToLower(u)
}

// Present reports whether a checkout's directory is still a git checkout.
func Present(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Inspect reads the facts registration needs from a directory.
func (s *Service) Inspect(ctx context.Context, path string) (kernel.RepoFacts, error) {
	path = ExpandPath(path)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return kernel.RepoFacts{}, fmt.Errorf("%s: %w", path, ErrNoSuchDir)
	}
	top, err := s.git(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return kernel.RepoFacts{}, fmt.Errorf("%s: %w", path, ErrNotGit)
	}
	top = filepath.Clean(top)
	if r, err := filepath.EvalSymlinks(top); err == nil {
		top = r
	}
	home := s.K.Cfg.Home
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	if inside(home, top) {
		return kernel.RepoFacts{}, fmt.Errorf("%s: %w", top, ErrInsideHome)
	}
	remote, _ := s.git(ctx, top, "config", "--get", "remote.origin.url")
	return kernel.RepoFacts{Path: top, Name: filepath.Base(top), Remote: NormalizeRemote(remote), DefaultBranch: s.defaultBranch(ctx, top)}, nil
}

func (s *Service) defaultBranch(ctx context.Context, dir string) string {
	if ref, err := s.git(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		if _, err := s.git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+strings.TrimPrefix(ref, "origin/")); err == nil {
			return strings.TrimPrefix(ref, "origin/")
		}
	}
	for _, b := range []string{"main", "master", "trunk"} {
		if _, err := s.git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err == nil {
			return b
		}
	}
	if cur, err := s.git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && cur != "HEAD" {
		return cur
	}
	return ""
}

// Register records a directory the person named.
func (s *Service) Register(ctx context.Context, path, source string) (kernel.Repo, error) {
	facts, err := s.Inspect(ctx, path)
	if err != nil {
		return kernel.Repo{}, err
	}
	r, _, err := s.K.RegisterRepo(ctx, facts, source)
	if err != nil {
		return r, err
	}
	s.repair(ctx, r)
	return r, nil
}

// Relocate: the person says where a repo they moved is now.
func (s *Service) Relocate(ctx context.Context, id, path, source string) (kernel.Repo, error) {
	facts, err := s.Inspect(ctx, path)
	if err != nil {
		return kernel.Repo{}, err
	}
	r, err := s.K.RelocateRepo(ctx, id, facts.Path, facts.Remote, facts.DefaultBranch, source)
	if err != nil {
		return r, err
	}
	s.repair(ctx, r)
	return r, nil
}

// repair points the worktrees of a moved repo back at it: each one's link to
// the repository is an absolute path that the move broke.
func (s *Service) repair(ctx context.Context, r kernel.Repo) {
	active, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{RepoID: r.ID, Status: kernel.CheckoutActive})
	if err != nil {
		return
	}
	var paths []string
	for _, c := range active {
		if c.Mode == kernel.CheckoutWorktree {
			paths = append(paths, c.Path)
		}
	}
	if len(paths) > 0 {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, _ = s.git(ctx, r.Path, append([]string{"worktree", "repair"}, paths...)...)
	}
}

// Forget removes a repo from the person's places. A repo still in place must
// have its checkouts archived first. For one that is gone, its checkouts are
// recorded as archived but their directories stay where they are: with the
// repository gone git cannot say what in them is unsaved, and forgetting never
// deletes anything.
func (s *Service) Forget(ctx context.Context, id, source string) error {
	r, err := s.K.GetRepo(ctx, id)
	if err != nil {
		return err
	}
	if Present(r.Path) {
		return s.K.ForgetRepo(ctx, id, source)
	}
	active, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{RepoID: id, Status: kernel.CheckoutActive})
	if err != nil {
		return err
	}
	for _, c := range active {
		detail := kernel.Payload{"reason": "repository forgotten"}
		if c.Mode == kernel.CheckoutWorktree && Present(c.Path) {
			detail["directoryKept"] = c.Path
		}
		if _, err := s.K.ArchiveCheckout(ctx, c.ID, source, detail); err != nil {
			return err
		}
	}
	return s.K.ForgetRepo(ctx, id, source)
}

// Check looks whether a registered repo is still where it was, records a
// change, and tells the person when it went missing.
func (s *Service) Check(ctx context.Context, r kernel.Repo, source string) (kernel.Repo, error) {
	status := kernel.RepoPresent
	if !Present(r.Path) {
		status = kernel.RepoMissing
	}
	updated, changed, err := s.K.SetRepoStatus(ctx, r.ID, status, source)
	if err != nil || !changed || status != kernel.RepoMissing {
		return updated, err
	}
	return updated, s.notifyMissing(ctx, updated)
}

// CheckAll checks every registered repo.
func (s *Service) CheckAll(ctx context.Context, source string) ([]kernel.Repo, error) {
	list, err := s.K.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]kernel.Repo, 0, len(list))
	for _, r := range list {
		updated, err := s.Check(ctx, r, source)
		if err != nil {
			return nil, err
		}
		out = append(out, updated)
	}
	return out, nil
}

// notifyMissing puts the question in front of the person: only they know
// whether the repo moved or is gone for good.
func (s *Service) notifyMissing(ctx context.Context, r kernel.Repo) error {
	question := fmt.Sprintf("仓库「%s」不在 %s 了。挪了位置的话告诉我新路径；不再需要的话，可以在任务页的「工作树」里移除它。", r.Name, r.Path)
	_, err := s.K.Append(ctx, kernel.EventInput{Source: "kernel:repos", Kind: "escalation", ThreadID: "main",
		// Not "path": an escalation's path is the chain of work items it climbed.
		Payload: kernel.Payload{"reason": "repo_missing", "question": question, "repoId": r.ID, "name": r.Name, "repoPath": r.Path,
			"root": "repo:" + kernel.GenID("chk", 6), "rootKind": "repo", "rootText": r.Name}})
	return err
}

// Resolve finds the repo a reference means: an id, a registered name, or a
// path (registered on first use). It never guesses between two candidates.
func (s *Service) Resolve(ctx context.Context, ref, source string) (kernel.Repo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return kernel.Repo{}, ErrUnknown
	}
	if strings.HasPrefix(ref, "repo_") {
		if r, err := s.K.GetRepo(ctx, ref); err == nil {
			return s.ensurePresent(ctx, r, source)
		}
	}
	list, err := s.K.ListRepos(ctx)
	if err != nil {
		return kernel.Repo{}, err
	}
	if LooksLikePath(ref) {
		path := ExpandPath(ref)
		for _, r := range list {
			if r.Path == path {
				return s.ensurePresent(ctx, r, source)
			}
		}
		return s.Register(ctx, path, source)
	}
	// "blog" fits both blog and blog-2 when both directories are called blog:
	// the person never saw the suffix, so that is a question, not a pick.
	var matches []kernel.Repo
	for _, r := range list {
		if strings.EqualFold(r.Name, ref) || strings.EqualFold(filepath.Base(r.Path), ref) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return kernel.Repo{}, fmt.Errorf("%q: %w", ref, ErrUnknown)
	case 1:
		return s.ensurePresent(ctx, matches[0], source)
	}
	return kernel.Repo{}, &AmbiguousError{Ref: ref, Candidates: matches}
}

func (s *Service) ensurePresent(ctx context.Context, r kernel.Repo, source string) (kernel.Repo, error) {
	r, err := s.Check(ctx, r, source)
	if err != nil {
		return r, err
	}
	if r.Status == kernel.RepoMissing {
		return r, &MissingError{Repo: r}
	}
	return r, nil
}

// AttachSpec says how a work item reaches a repo.
type AttachSpec struct {
	// Base is the ref a new branch starts from (default: the repo's default branch).
	Base string
	// From is a work item whose branch in this repo the new one continues.
	From string
	// InPlace works in the person's own directory instead of a worktree —
	// only when they asked for it.
	InPlace bool
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func dirName(name string) string {
	n := strings.TrimLeft(unsafeName.ReplaceAllString(name, "-"), ".-")
	if n == "" {
		n = "repo"
	}
	return n
}

// BranchFor is the branch a work item's worktree of any repo works on.
func BranchFor(workItemID string) string { return "hidane/" + workItemID }

// Attach gives a work item access to a repo: a worktree of its own inside the
// work item's workspace by default, the original directory only on request.
// Attaching a repo the item already has returns that checkout.
func (s *Service) Attach(ctx context.Context, item kernel.WorkItem, r kernel.Repo, spec AttachSpec, source string) (kernel.Checkout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID, RepoID: r.ID, Status: kernel.CheckoutActive})
	if err != nil {
		return kernel.Checkout{}, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	if !Present(r.Path) {
		return kernel.Checkout{}, &MissingError{Repo: r}
	}
	if spec.InPlace {
		branch, _ := s.git(ctx, r.Path, "rev-parse", "--abbrev-ref", "HEAD")
		return s.K.CreateCheckout(ctx, kernel.Checkout{WorkItemID: item.ID, RepoID: r.ID, Mode: kernel.CheckoutInPlace,
			Path: r.Path, Branch: branch}, r.Name, source)
	}
	dir := filepath.Join(item.Workspace, dirName(r.Name))
	for i := 2; ; i++ {
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			break
		}
		dir = filepath.Join(item.Workspace, fmt.Sprintf("%s-%d", dirName(r.Name), i))
	}
	branch := BranchFor(item.ID)
	var args []string
	base := ""
	if _, err := s.git(ctx, r.Path, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		// The item worked here before and archived it: its branch carries on.
		args = []string{"worktree", "add", dir, branch}
		base = branch
	} else {
		start, err := s.startPoint(ctx, r, spec)
		if err != nil {
			return kernel.Checkout{}, err
		}
		args = []string{"worktree", "add", "-b", branch, dir, start}
		base = start
	}
	if err := os.MkdirAll(item.Workspace, 0o755); err != nil {
		return kernel.Checkout{}, err
	}
	// A worktree directory someone deleted by hand still holds its branch in
	// git's eyes until pruned, and the branch could not be checked out again.
	_, _ = s.git(ctx, r.Path, "worktree", "prune")
	if _, err := s.git(ctx, r.Path, args...); err != nil {
		return kernel.Checkout{}, err
	}
	setup := kernel.SetupNone
	if len(s.Config(r, dir).Setup) > 0 {
		setup = kernel.SetupPending
	}
	c, err := s.K.CreateCheckout(ctx, kernel.Checkout{WorkItemID: item.ID, RepoID: r.ID, Mode: kernel.CheckoutWorktree,
		Path: dir, Branch: branch, Base: base, Setup: setup}, r.Name, source)
	if err != nil {
		// A worktree nobody records is one nobody would ever clean up.
		_, _ = s.git(ctx, r.Path, "worktree", "remove", "--force", dir)
		return c, err
	}
	return c, nil
}

func (s *Service) startPoint(ctx context.Context, r kernel.Repo, spec AttachSpec) (string, error) {
	start := strings.TrimSpace(spec.Base)
	if spec.From != "" {
		prior, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: spec.From, RepoID: r.ID})
		if err != nil {
			return "", err
		}
		if len(prior) == 0 {
			return "", fmt.Errorf("work item %s never worked on %s", spec.From, r.Name)
		}
		start = prior[len(prior)-1].Branch
	}
	if start == "" {
		start = r.DefaultBranch
	}
	if start == "" {
		start = "HEAD"
	}
	if _, err := s.git(ctx, r.Path, "rev-parse", "--verify", "--quiet", start+"^{commit}"); err != nil {
		return "", fmt.Errorf("%s has no branch or commit %q", r.Name, start)
	}
	return start, nil
}

// Inherit gives a child work item its parent's repos, each on a branch of its
// own started from where the parent's work is, so the parent can merge it back.
func (s *Service) Inherit(ctx context.Context, parent, child kernel.WorkItem, only []string, source string) ([]kernel.Checkout, []error) {
	active, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: parent.ID, Status: kernel.CheckoutActive})
	if err != nil {
		return nil, []error{err}
	}
	var out []kernel.Checkout
	var errs []error
	for _, c := range active {
		r, err := s.K.GetRepo(ctx, c.RepoID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if only != nil && !nameIn(r.Name, only) {
			continue
		}
		spec := AttachSpec{From: parent.ID}
		if c.Mode == kernel.CheckoutInPlace {
			spec = AttachSpec{Base: c.Branch}
		}
		got, err := s.Attach(ctx, child, r, spec, source)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, err))
			continue
		}
		out = append(out, got)
	}
	return out, errs
}

func nameIn(name string, list []string) bool {
	for _, n := range list {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

// Status is a checkout as the person and the agents see it now.
type Status struct {
	kernel.Checkout
	RepoName   string `json:"repoName"`
	RepoPath   string `json:"repoPath"`
	RepoStatus string `json:"repoStatus"`
	// Health: ok, missing (the directory is gone) or repo_missing.
	Health string `json:"health"`
	// Ahead counts commits on the branch that the repo's default branch does
	// not have; Dirty counts uncommitted changes. Both are -1 when they cannot
	// be read.
	Ahead int    `json:"ahead"`
	Dirty int    `json:"dirty"`
	Head  string `json:"head"`
}

// Describe reads a checkout's live state from disk.
func (s *Service) Describe(ctx context.Context, c kernel.Checkout, r kernel.Repo) Status {
	st := Status{Checkout: c, RepoName: r.Name, RepoPath: r.Path, RepoStatus: r.Status, Health: "ok", Ahead: -1, Dirty: -1}
	if c.Status != kernel.CheckoutActive {
		return st
	}
	switch {
	case r.Status == kernel.RepoMissing || !Present(r.Path):
		st.Health = "repo_missing"
		return st
	case !Present(c.Path):
		st.Health = "missing"
		return st
	}
	if out, err := s.git(ctx, c.Path, "status", "--porcelain"); err == nil {
		st.Dirty = countLines(out)
	}
	if head, err := s.git(ctx, c.Path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		st.Head = head
	}
	// Ahead of the trunk, not of where the branch started: a task continued
	// from another task's branch carries that work too.
	trunk := r.DefaultBranch
	if trunk == "" {
		trunk = c.Base
	}
	if c.Mode == kernel.CheckoutWorktree && trunk != "" && trunk != c.Branch {
		if out, err := s.git(ctx, c.Path, "rev-list", "--count", trunk+"..HEAD"); err == nil {
			if n, err := strconv.Atoi(out); err == nil {
				st.Ahead = n
			}
		}
	}
	return st
}

func countLines(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(s, "\n"), "\n"))
}

// Archive removes a worktree from disk, keeping its branch, after the repo's
// teardown script; an in-place checkout only gives the directory back. A
// directory is deleted only when git can tell what is unsaved in it: never
// with uncommitted changes unless forced, and never when its repository is gone.
func (s *Service) Archive(ctx context.Context, id string, force bool, source string) (kernel.Checkout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.K.GetCheckout(ctx, id)
	if err != nil || c.Status != kernel.CheckoutActive {
		return c, err
	}
	if c.Mode == kernel.CheckoutInPlace {
		return s.K.ArchiveCheckout(ctx, id, source, kernel.Payload{"released": true})
	}
	detail := kernel.Payload{"branchKept": c.Branch != ""}
	r, rerr := s.K.GetRepo(ctx, c.RepoID)
	repoHere := rerr == nil && Present(r.Path)
	if !Present(c.Path) {
		if repoHere {
			_, _ = s.git(ctx, r.Path, "worktree", "prune")
		}
		return s.K.ArchiveCheckout(ctx, id, source, detail)
	}
	readable := false
	if repoHere {
		if out, err := s.git(ctx, c.Path, "status", "--porcelain"); err == nil {
			readable = true
			if n := countLines(out); n > 0 && !force {
				return c, &DirtyError{Files: n}
			}
		}
	}
	if !readable {
		detail["directoryKept"] = c.Path
		return s.K.ArchiveCheckout(ctx, id, source, detail)
	}
	teardown := s.Config(r, c.Path).Teardown
	if len(teardown) == 0 {
		detail["teardown"] = "none"
	} else {
		item, err := s.K.GetWorkItem(ctx, c.WorkItemID)
		if err != nil {
			return c, err
		}
		in := kernel.EventInput{Source: source, ThreadID: item.ThreadID, WorkItemID: item.ID}
		in.Kind, in.Payload = "side_effect.intent", kernel.Payload{"tool": "teardown", "input": strings.Join(teardown, "\n"), "checkoutId": c.ID, "repo": r.Name}
		if _, err := s.K.Append(ctx, in); err != nil {
			return c, err
		}
		runErr := s.runScript(ctx, c, r, teardown, "teardown")
		in.Kind, in.Payload = "side_effect.result", kernel.Payload{"tool": "teardown", "isError": runErr != nil, "checkoutId": c.ID, "log": LogPath(item, c, "teardown")}
		if _, err := s.K.Append(context.WithoutCancel(ctx), in); err != nil {
			return c, err
		}
		detail["teardown"] = "ok"
		if runErr != nil {
			detail["teardown"] = "failed"
		}
	}
	if _, err := s.git(ctx, r.Path, "worktree", "remove", "--force", c.Path); err != nil {
		detail["removeError"] = err.Error()
		// What git would not remove is still only ever inside the work item's workspace.
		if inside(s.K.Cfg.WorkspacesDir(), c.Path) {
			_ = os.RemoveAll(c.Path)
		}
	}
	_, _ = s.git(ctx, r.Path, "worktree", "prune")
	return s.K.ArchiveCheckout(ctx, id, source, detail)
}

// LogPath is where a checkout's setup and teardown output goes.
func LogPath(item kernel.WorkItem, c kernel.Checkout, phase string) string {
	return filepath.Join(item.Workspace, ".hidane", phase+"-"+c.ID+".log")
}

// SetupResult is how a setup script ended.
type SetupResult struct {
	Ran     bool
	OK      bool
	LogPath string
	Tail    string
}

// RunSetup runs a fresh worktree's setup script once, before the first worker
// that needs it, recording it as a side effect of that execution.
func (s *Service) RunSetup(ctx context.Context, item kernel.WorkItem, c kernel.Checkout, executionID string) SetupResult {
	if c.Setup != kernel.SetupPending {
		return SetupResult{}
	}
	// ctx stops the script; what happened is recorded regardless.
	record := context.WithoutCancel(ctx)
	r, err := s.K.GetRepo(record, c.RepoID)
	if err != nil {
		return SetupResult{}
	}
	commands := s.Config(r, c.Path).Setup
	if len(commands) == 0 {
		_ = s.K.SetCheckoutSetup(record, c.ID, kernel.SetupNone)
		return SetupResult{}
	}
	_ = s.K.SetCheckoutSetup(record, c.ID, kernel.SetupRunning)
	in := kernel.EventInput{Source: "agent:worker", ThreadID: item.ThreadID, WorkItemID: item.ID, ExecutionID: executionID}
	in.Kind, in.Payload = "side_effect.intent", kernel.Payload{"tool": "setup", "input": strings.Join(commands, "\n"), "checkoutId": c.ID, "repo": r.Name}
	_, _ = s.K.Append(record, in)
	runErr := s.runScript(ctx, c, r, commands, "setup")
	log := LogPath(item, c, "setup")
	in.Kind, in.Payload = "side_effect.result", kernel.Payload{"tool": "setup", "isError": runErr != nil, "checkoutId": c.ID, "log": log}
	_, _ = s.K.Append(record, in)
	state := kernel.SetupDone
	if runErr != nil {
		state = kernel.SetupFailed
	}
	_ = s.K.SetCheckoutSetup(record, c.ID, state)
	return SetupResult{Ran: true, OK: runErr == nil, LogPath: log, Tail: tail(log, 2000)}
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.TrimSpace(string(b))
}

// runScript runs commands one after another in the checkout, stopping at the
// first failure; output goes to the checkout's log in the workspace.
func (s *Service) runScript(ctx context.Context, c kernel.Checkout, r kernel.Repo, commands []string, phase string) error {
	item, err := s.K.GetWorkItem(context.WithoutCancel(ctx), c.WorkItemID)
	if err != nil {
		return err
	}
	logPath := LogPath(item, c, phase)
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	ctx, cancel := context.WithTimeout(ctx, s.SetupTimeout)
	defer cancel()
	env := append(s.environ(),
		"HIDANE_SOURCE_CHECKOUT_PATH="+r.Path, "HIDANE_WORKTREE_PATH="+c.Path,
		"HIDANE_BRANCH="+c.Branch, "HIDANE_WORK_ITEM_ID="+c.WorkItemID,
		// A repo set up for Paseo already says what its worktrees need.
		"PASEO_SOURCE_CHECKOUT_PATH="+r.Path, "PASEO_WORKTREE_PATH="+c.Path, "PASEO_BRANCH_NAME="+c.Branch)
	for _, command := range commands {
		fmt.Fprintf(logFile, "$ %s\n", command)
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", command)
		} else {
			cmd = exec.CommandContext(ctx, "/bin/sh", "-e", "-c", command)
		}
		cmd.Dir, cmd.Env = c.Path, env
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.WaitDelay = 5 * time.Second
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				err = fmt.Errorf("timed out after %s", s.SetupTimeout)
			}
			fmt.Fprintf(logFile, "%s failed: %v\n", phase, err)
			return err
		}
	}
	return nil
}
