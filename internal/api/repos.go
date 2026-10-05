package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/repos"
)

// The person's repositories and the worktrees their work items hold: listed
// with their live state, and archived by hand — nothing is cleaned up behind
// the person's back.

func (s *server) repos(w http.ResponseWriter, r *http.Request) {
	list, err := s.Sys.Repos.CheckAll(r.Context(), "connector:web")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": list})
}

func (s *server) addRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("path required"))
		return
	}
	repo, err := s.Sys.Repos.Register(r.Context(), body.Path, "connector:web")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(agents.RepoProblem(body.Path, err)))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "repo": repo})
}

func (s *server) patchRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("path required"))
		return
	}
	repo, err := s.Sys.Repos.Relocate(r.Context(), r.PathValue("id"), body.Path, "connector:web")
	switch {
	case notFound(err):
		writeJSON(w, http.StatusNotFound, errBody("not found"))
	case errors.Is(err, kernel.ErrPathTaken):
		writeJSON(w, http.StatusConflict, errBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusBadRequest, errBody(agents.RepoProblem(body.Path, err)))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repo": repo})
	}
}

func (s *server) deleteRepo(w http.ResponseWriter, r *http.Request) {
	err := s.Sys.Repos.Forget(r.Context(), r.PathValue("id"), "connector:web")
	switch {
	case notFound(err):
		writeJSON(w, http.StatusNotFound, errBody("not found"))
	case errors.Is(err, kernel.ErrRepoInUse):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error(), "inUse": true})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// CheckoutView is a checkout with the work item it belongs to.
type CheckoutView struct {
	repos.Status
	Title          string `json:"title"`
	ItemStatus     string `json:"itemStatus"`
	Running        bool   `json:"running"`
	LastActivityAt string `json:"lastActivityAt"`
}

func (s *server) describeCheckouts(ctx context.Context, list []kernel.Checkout) ([]CheckoutView, error) {
	// A repo gone missing is noticed by whoever looks first, not only the repo list.
	repoList, err := s.Sys.Repos.CheckAll(ctx, "connector:web")
	if err != nil {
		return nil, err
	}
	byID := map[string]kernel.Repo{}
	for _, rp := range repoList {
		byID[rp.ID] = rp
	}
	busy, err := s.K.BusyWorkItemIDs(ctx)
	if err != nil {
		return nil, err
	}
	isBusy := map[string]bool{}
	for _, id := range busy {
		isBusy[id] = true
	}
	out := []CheckoutView{}
	for _, c := range list {
		rp, ok := byID[c.RepoID]
		if !ok {
			rp = kernel.Repo{ID: c.RepoID, Name: c.RepoID, Status: kernel.RepoMissing}
		}
		v := CheckoutView{Status: s.Sys.Repos.Describe(ctx, c, rp), Running: isBusy[c.WorkItemID], LastActivityAt: c.UpdatedAt}
		if item, err := s.K.GetWorkItem(ctx, c.WorkItemID); err == nil {
			v.Title, v.ItemStatus = item.Title, item.Status
		}
		var last *string
		_ = s.K.DB.QueryRowContext(ctx, `SELECT max(ts) FROM events WHERE work_item_id = ?`, c.WorkItemID).Scan(&last)
		if last != nil && *last > v.LastActivityAt {
			v.LastActivityAt = *last
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *server) checkouts(w http.ResponseWriter, r *http.Request) {
	status := kernel.CheckoutActive
	if has(r, "all") {
		status = ""
	}
	list, err := s.K.ListCheckouts(r.Context(), kernel.CheckoutFilter{Status: status, WorkItemID: r.URL.Query().Get("item")})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	views, err := s.describeCheckouts(r.Context(), list)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	// Newest work first: the page is for deciding what to clean up.
	for i, j := 0, len(views)-1; i < j; i, j = i+1, j-1 {
		views[i], views[j] = views[j], views[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkouts": views})
}

func (s *server) archiveCheckout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Force bool `json:"force"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid json"))
		return
	}
	ctx := r.Context()
	c, err := s.K.GetCheckout(ctx, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	// A worker writing into the directory must not have it removed under it.
	if _, running, err := s.K.ActiveExecutionFor(ctx, c.WorkItemID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	} else if running {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "the work item is running; stop it first", "running": true})
		return
	}
	archived, err := s.Sys.Repos.Archive(ctx, c.ID, body.Force, "connector:web")
	var dirty *repos.DirtyError
	switch {
	case errors.As(err, &dirty):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error(), "dirty": dirty.Files})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "checkout": archived})
	}
}

// changes: what a task changed in each repository it works in, read from git
// for the person's review — committed and not, since the branch left the trunk.
func (s *server) changes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	item, err := s.K.GetWorkItem(ctx, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("not found"))
		return
	}
	held, err := s.K.ListCheckouts(ctx, kernel.CheckoutFilter{WorkItemID: item.ID})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	out := []repos.Changes{}
	for _, c := range held {
		if c.Status != kernel.CheckoutActive {
			continue
		}
		repo, err := s.K.GetRepo(ctx, c.RepoID)
		if err != nil {
			repo = kernel.Repo{ID: c.RepoID, Name: c.RepoID, Status: kernel.RepoMissing}
		}
		out = append(out, s.Sys.Repos.Changes(ctx, c, repo))
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkouts": out})
}
