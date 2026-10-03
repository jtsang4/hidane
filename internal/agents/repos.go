package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/repos"
)

// What the roles are told about the person's repositories and a work item's
// checkouts, and how a request for a repo becomes a checkout.

type itemCheckouts struct {
	list  []kernel.Checkout
	repos map[string]kernel.Repo
}

func (s *System) checkoutsOf(ctx context.Context, itemID, status string) (itemCheckouts, error) {
	list, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: itemID, Status: status})
	if err != nil {
		return itemCheckouts{}, err
	}
	out := itemCheckouts{list: list, repos: map[string]kernel.Repo{}}
	for _, c := range list {
		if _, ok := out.repos[c.RepoID]; ok {
			continue
		}
		if r, err := s.K.GetRepo(ctx, c.RepoID); err == nil {
			out.repos[c.RepoID] = r
		} else {
			out.repos[c.RepoID] = kernel.Repo{ID: c.RepoID, Name: c.RepoID, Status: kernel.RepoMissing}
		}
	}
	return out, nil
}

// checkoutsByItem reads every checkout at once, grouped by work item.
func (s *System) checkoutsByItem(ctx context.Context) (map[string]itemCheckouts, error) {
	list, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{})
	if err != nil {
		return nil, err
	}
	all, err := s.K.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]kernel.Repo{}
	for _, r := range all {
		byID[r.ID] = r
	}
	out := map[string]itemCheckouts{}
	for _, c := range list {
		ic := out[c.WorkItemID]
		if ic.repos == nil {
			ic.repos = map[string]kernel.Repo{}
		}
		ic.list = append(ic.list, c)
		r, ok := byID[c.RepoID]
		if !ok {
			r = kernel.Repo{ID: c.RepoID, Name: c.RepoID, Status: kernel.RepoMissing}
		}
		ic.repos[c.RepoID] = r
		out[c.WorkItemID] = ic
	}
	return out, nil
}

// short is one checkout in a few words, for an inventory line.
func (ic itemCheckouts) short() string {
	var parts []string
	for _, c := range ic.list {
		r := ic.repos[c.RepoID]
		switch {
		case c.Status == kernel.CheckoutArchived && c.Mode == kernel.CheckoutWorktree:
			parts = append(parts, fmt.Sprintf("%s (archived; branch %s kept)", r.Name, c.Branch))
		case c.Status == kernel.CheckoutArchived:
			continue
		case c.Mode == kernel.CheckoutInPlace:
			parts = append(parts, fmt.Sprintf("%s (in the person's own directory, branch %s)", r.Name, c.Branch))
		default:
			parts = append(parts, fmt.Sprintf("%s@%s", r.Name, c.Branch))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "repos: " + strings.Join(parts, ", ")
}

// long describes the item's active checkouts for its Manager and workers,
// and a worktree archived earlier that the item could pick up again.
func (ic itemCheckouts) long() string {
	var lines []string
	active := map[string]bool{}
	for _, c := range ic.list {
		if c.Status == kernel.CheckoutActive {
			active[c.RepoID] = true
		}
	}
	hinted := map[string]bool{}
	for _, c := range ic.list {
		if c.Status != kernel.CheckoutActive {
			if c.Mode == kernel.CheckoutWorktree && !active[c.RepoID] && !hinted[c.RepoID] {
				hinted[c.RepoID] = true
				name := ic.repos[c.RepoID].Name
				lines = append(lines, fmt.Sprintf("- %s: the person archived its worktree (directory removed; branch %s kept). Only if they ask to work on it again, attach_repo %q puts it back on that branch.", name, c.Branch, name))
			}
			continue
		}
		r := ic.repos[c.RepoID]
		var l string
		if c.Mode == kernel.CheckoutInPlace {
			l = fmt.Sprintf("- %s: %s — the person's own directory, worked on in place at their request (branch %s). Do not commit there unless asked.", r.Name, c.Path, c.Branch)
		} else {
			l = fmt.Sprintf("- %s: %s — this work item's own git worktree on branch %s", r.Name, c.Path, c.Branch)
			if c.Base != "" && c.Base != c.Branch {
				l += " (started from " + c.Base + ")"
			}
		}
		switch c.Setup {
		case kernel.SetupPending:
			l += "; its setup script runs before the next worker"
		case kernel.SetupFailed:
			l += "; its setup script FAILED (see .hidane/setup-" + c.ID + ".log in the workspace)"
		}
		if r.Status == kernel.RepoMissing {
			l += " [the repository is missing from " + r.Path + "]"
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// repoInventory lists the person's registered repositories.
func (s *System) repoInventory(ctx context.Context) string {
	list, err := s.K.ListRepos(ctx)
	if err != nil || len(list) == 0 {
		return "(none registered yet)"
	}
	var lines []string
	for _, r := range list {
		l := fmt.Sprintf("- %s %s: %s", r.ID, r.Name, r.Path)
		if r.DefaultBranch != "" {
			l += " (default branch " + r.DefaultBranch + ")"
		}
		if r.Status == kernel.RepoMissing {
			l += " [missing: no longer at this path]"
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// RepoRequest is one repository a work item is asked to work in.
type RepoRequest struct {
	Ref  string
	Spec repos.AttachSpec
}

// parseRepoRequests reads "repos" (objects or plain names) and the older
// single "repo" path.
func parseRepoRequests(e Effect) []RepoRequest {
	var out []RepoRequest
	if list, ok := e["repos"].([]any); ok {
		for _, raw := range list {
			switch v := raw.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					out = append(out, RepoRequest{Ref: v})
				}
			case map[string]any:
				ref := Str(v["repo"])
				if ref == "" {
					ref = Str(v["path"])
				}
				if strings.TrimSpace(ref) == "" {
					continue
				}
				inPlace, _ := v["in_place"].(bool)
				out = append(out, RepoRequest{Ref: ref, Spec: repos.AttachSpec{Base: Str(v["base"]), From: Str(v["from"]), InPlace: inPlace}})
			}
		}
	}
	if ref := Str(e["repo"]); strings.TrimSpace(ref) != "" {
		out = append(out, RepoRequest{Ref: ref})
	}
	return out
}

type resolvedRepo struct {
	repo kernel.Repo
	spec repos.AttachSpec
}

// resolveRepos settles which repositories are meant before anything is
// created. A problem comes back as the question to put to the person: work
// never starts on a guess about which repo, which branch, or a repo that is gone.
func (s *System) resolveRepos(ctx context.Context, reqs []RepoRequest, source string) ([]resolvedRepo, string) {
	var out []resolvedRepo
	seen := map[string]bool{}
	for _, rq := range reqs {
		r, err := s.Repos.Resolve(ctx, rq.Ref, source)
		if err != nil {
			return nil, RepoProblem(rq.Ref, err)
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if rq.Spec.From != "" {
			prior, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: rq.Spec.From, RepoID: r.ID})
			if err != nil {
				return nil, err.Error()
			}
			if len(prior) == 0 {
				return nil, fmt.Sprintf("任务 %s 没有在仓库「%s」上工作过，没有可以接着做的分支。要从哪个分支开始？", rq.Spec.From, r.Name)
			}
		}
		if rq.Spec.InPlace {
			active, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{RepoID: r.ID, Status: kernel.CheckoutActive})
			if err != nil {
				return nil, err.Error()
			}
			for _, c := range active {
				if c.Mode == kernel.CheckoutInPlace {
					holder := c.WorkItemID
					if it, err := s.K.GetWorkItem(ctx, c.WorkItemID); err == nil {
						holder = fmt.Sprintf("「%s」(%s)", it.Title, it.ID)
					}
					return nil, fmt.Sprintf("仓库「%s」的原目录正被任务 %s 使用。要等它结束，还是改在新的工作树里做？", r.Name, holder)
				}
			}
		}
		out = append(out, resolvedRepo{repo: r, spec: rq.Spec})
	}
	return out, ""
}

// RepoProblem says in the person's words why a repository cannot be used.
func RepoProblem(ref string, err error) string {
	var amb *repos.AmbiguousError
	var missing *repos.MissingError
	switch {
	case errors.As(err, &amb):
		var names []string
		for _, c := range amb.Candidates {
			names = append(names, fmt.Sprintf("%s（%s）", c.Name, c.Path))
		}
		return fmt.Sprintf("「%s」对应多个仓库：%s。是哪一个？", ref, strings.Join(names, "、"))
	case errors.As(err, &missing):
		return fmt.Sprintf("仓库「%s」不在 %s 了。挪了位置的话告诉我新路径；不再需要的话我就把它从列表里移除。", missing.Repo.Name, missing.Repo.Path)
	case errors.Is(err, repos.ErrUnknown):
		return fmt.Sprintf("没有登记过叫「%s」的仓库。它在本机的哪个路径？", ref)
	case errors.Is(err, repos.ErrNoSuchDir):
		return fmt.Sprintf("找不到目录 %s。路径对吗？", repos.ExpandPath(ref))
	case errors.Is(err, repos.ErrNotGit):
		return fmt.Sprintf("%s 不是 git 仓库，没法在它上面开工作树。", repos.ExpandPath(ref))
	case errors.Is(err, repos.ErrInsideHome):
		return fmt.Sprintf("%s 在 hidane 自己的数据目录里，不能当作仓库使用。", repos.ExpandPath(ref))
	}
	return fmt.Sprintf("无法使用仓库「%s」：%v", ref, err)
}

// attachAll gives a new work item its repositories. The first failure is
// returned; checkouts made before it stay attached.
func (s *System) attachAll(ctx context.Context, item kernel.WorkItem, list []resolvedRepo, source string) error {
	for _, rr := range list {
		if _, err := s.Repos.Attach(ctx, item, rr.repo, rr.spec, source); err != nil {
			return fmt.Errorf("%s: %w", rr.repo.Name, err)
		}
	}
	return nil
}

// StartWorkItem creates a work item in the repositories it asks for. When the
// repositories cannot be settled, question is what to ask the person and
// nothing is created; when a checkout fails after all, the item is closed
// again and question says why — work never starts half-prepared.
func (s *System) StartWorkItem(ctx context.Context, title, source string, opts kernel.CreateWorkItemOpts, reqs []RepoRequest) (kernel.WorkItem, string, error) {
	wanted, problem := s.resolveRepos(ctx, reqs, source)
	if problem != "" {
		return kernel.WorkItem{}, problem, nil
	}
	item, err := s.K.CreateWorkItem(ctx, title, source, opts)
	if err != nil {
		return item, "", err
	}
	if err := s.attachAll(ctx, item, wanted, source); err != nil {
		if item, err = s.K.SetWorkItemStatus(ctx, item.ID, kernel.StatusClosed, source); err != nil {
			return item, "", err
		}
		return item, fmt.Sprintf("没能准备好仓库，任务 %s 没有开始：%v", item.ID, err), nil
	}
	return item, "", nil
}
