package kernel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"strings"
)

// Repos are places the person keeps outside hidane; checkouts are a work
// item's access to one of them. The kernel keeps the state and the facts —
// how a checkout is materialized (git) lives outside it.

const (
	RepoPresent = "present"
	RepoMissing = "missing"

	CheckoutWorktree = "worktree"
	CheckoutInPlace  = "in_place"

	CheckoutActive   = "active"
	CheckoutArchived = "archived"

	SetupNone    = "none"
	SetupPending = "pending"
	SetupRunning = "running"
	SetupDone    = "done"
	SetupFailed  = "failed"
)

type Repo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Remote        string `json:"remote"`
	DefaultBranch string `json:"defaultBranch"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// RepoFacts is what was observed about a directory when it was registered.
type RepoFacts struct {
	Path          string
	Name          string
	Remote        string
	DefaultBranch string
}

type Checkout struct {
	ID         string `json:"id"`
	WorkItemID string `json:"workItemId"`
	RepoID     string `json:"repoId"`
	Mode       string `json:"mode"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Base       string `json:"base"`
	Status     string `json:"status"`
	Setup      string `json:"setup"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

var (
	// ErrRepoInUse: a present repo with active checkouts cannot be forgotten.
	ErrRepoInUse = errors.New("the repository has active checkouts")
	// ErrPathTaken: another registered repo already lives at that path.
	ErrPathTaken = errors.New("another repository is registered at that path")
)

// LeaseError: someone else is working in the person's original directory.
type LeaseError struct{ Holder string }

func (e *LeaseError) Error() string {
	return fmt.Sprintf("the original directory is in use by work item %s", e.Holder)
}

const repoCols = `id, name, path, remote, default_branch, status, created_at, updated_at`

func scanRepo(s scanner) (Repo, error) {
	var r Repo
	var remote, branch sql.NullString
	err := s.Scan(&r.ID, &r.Name, &r.Path, &remote, &branch, &r.Status, &r.CreatedAt, &r.UpdatedAt)
	r.Remote, r.DefaultBranch = str(remote), str(branch)
	return r, err
}

func (k *Kernel) queryRepos(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, q string, args ...any) ([]Repo, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Repo{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (k *Kernel) ListRepos(ctx context.Context) ([]Repo, error) {
	return k.queryRepos(ctx, k.DB, `SELECT `+repoCols+` FROM repos ORDER BY name COLLATE NOCASE ASC, created_at ASC`)
}

func (k *Kernel) GetRepo(ctx context.Context, id string) (Repo, error) {
	r, err := scanRepo(k.DB.QueryRowContext(ctx, `SELECT `+repoCols+` FROM repos WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("repository not found: %s: %w", id, ErrNotFound)
	}
	return r, err
}

// RegisterRepo records a directory as one of the person's places. The same
// path is the same repo. A repo whose remote matches one that went missing is
// that repo, moved: it keeps its id, so its history and checkouts follow it.
func (k *Kernel) RegisterRepo(ctx context.Context, f RepoFacts, source string) (Repo, bool, error) {
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return Repo{}, false, err
	}
	defer tx.Rollback()
	now := k.stamp()
	if found, err := k.queryRepos(ctx, tx, `SELECT `+repoCols+` FROM repos WHERE path = ?`, f.Path); err != nil {
		return Repo{}, false, err
	} else if len(found) > 0 {
		r := found[0]
		if _, err := tx.ExecContext(ctx, `UPDATE repos SET remote = ?, default_branch = ?, status = ?, updated_at = ? WHERE id = ?`,
			nullable(f.Remote), nullable(f.DefaultBranch), RepoPresent, now, r.ID); err != nil {
			return Repo{}, false, err
		}
		var ev Event
		if r.Status == RepoMissing {
			if ev, err = k.appendTx(ctx, tx, EventInput{Source: source, Kind: "repo.found",
				Payload: Payload{"repoId": r.ID, "name": r.Name, "path": r.Path}}); err != nil {
				return Repo{}, false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return Repo{}, false, err
		}
		if ev.Seq > 0 {
			k.Hub.Publish(ev.Seq)
		}
		r, err = k.GetRepo(ctx, r.ID)
		return r, false, err
	}
	if f.Remote != "" {
		moved, err := k.queryRepos(ctx, tx, `SELECT `+repoCols+` FROM repos WHERE remote = ? AND status = ? ORDER BY created_at ASC LIMIT 1`,
			f.Remote, RepoMissing)
		if err != nil {
			return Repo{}, false, err
		}
		if len(moved) > 0 {
			r := moved[0]
			if _, err := tx.ExecContext(ctx, `UPDATE repos SET path = ?, default_branch = ?, status = ?, updated_at = ? WHERE id = ?`,
				f.Path, nullable(f.DefaultBranch), RepoPresent, now, r.ID); err != nil {
				return Repo{}, false, err
			}
			ev, err := k.appendTx(ctx, tx, EventInput{Source: source, Kind: "repo.relocated",
				Payload: Payload{"repoId": r.ID, "name": r.Name, "from": r.Path, "to": f.Path}})
			if err != nil {
				return Repo{}, false, err
			}
			if err := tx.Commit(); err != nil {
				return Repo{}, false, err
			}
			k.Hub.Publish(ev.Seq)
			r, err = k.GetRepo(ctx, r.ID)
			return r, false, err
		}
	}
	name, err := uniqueRepoName(ctx, tx, f.Name)
	if err != nil {
		return Repo{}, false, err
	}
	id := GenID("repo", 6)
	if _, err := tx.ExecContext(ctx, `INSERT INTO repos (id, name, path, remote, default_branch, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, name, f.Path, nullable(f.Remote), nullable(f.DefaultBranch), RepoPresent, now, now); err != nil {
		return Repo{}, false, err
	}
	ev, err := k.appendTx(ctx, tx, EventInput{Source: source, Kind: "repo.registered",
		Payload: Payload{"repoId": id, "name": name, "path": f.Path, "remote": nullable(f.Remote), "defaultBranch": nullable(f.DefaultBranch)}})
	if err != nil {
		return Repo{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Repo{}, false, err
	}
	k.Hub.Publish(ev.Seq)
	r, err := k.GetRepo(ctx, id)
	return r, true, err
}

// uniqueRepoName: two clones both called "blog" need names a person can tell apart.
func uniqueRepoName(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "repo"
	}
	name := base
	for i := 2; ; i++ {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM repos WHERE name = ? COLLATE NOCASE`, name).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return name, nil
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// SetRepoStatus records a repo found missing or back in place; nothing is
// recorded when the status did not change.
func (k *Kernel) SetRepoStatus(ctx context.Context, id, status, source string) (Repo, bool, error) {
	r, err := k.GetRepo(ctx, id)
	if err != nil || r.Status == status {
		return r, false, err
	}
	if _, err := k.DB.ExecContext(ctx, `UPDATE repos SET status = ?, updated_at = ? WHERE id = ?`, status, k.stamp(), id); err != nil {
		return r, false, err
	}
	kind := "repo.found"
	if status == RepoMissing {
		kind = "repo.missing"
	}
	if _, err := k.Append(ctx, EventInput{Source: source, Kind: kind, Payload: Payload{"repoId": r.ID, "name": r.Name, "path": r.Path}}); err != nil {
		return r, false, err
	}
	r, err = k.GetRepo(ctx, id)
	return r, true, err
}

// RelocateRepo: the person says where a repo is now.
func (k *Kernel) RelocateRepo(ctx context.Context, id, path, remote, defaultBranch, source string) (Repo, error) {
	r, err := k.GetRepo(ctx, id)
	if err != nil {
		return r, err
	}
	var other string
	err = k.DB.QueryRowContext(ctx, `SELECT id FROM repos WHERE path = ? AND id != ?`, path, id).Scan(&other)
	if err == nil {
		return r, ErrPathTaken
	} else if err != sql.ErrNoRows {
		return r, err
	}
	if _, err := k.DB.ExecContext(ctx, `UPDATE repos SET path = ?, remote = ?, default_branch = ?, status = ?, updated_at = ? WHERE id = ?`,
		path, nullable(remote), nullable(defaultBranch), RepoPresent, k.stamp(), id); err != nil {
		return r, err
	}
	if _, err := k.Append(ctx, EventInput{Source: source, Kind: "repo.relocated",
		Payload: Payload{"repoId": r.ID, "name": r.Name, "from": r.Path, "to": path}}); err != nil {
		return r, err
	}
	return k.GetRepo(ctx, id)
}

// ForgetRepo removes a repo from the person's places. Its checkouts must have
// been archived first, unless the repo itself is gone.
func (k *Kernel) ForgetRepo(ctx context.Context, id, source string) error {
	r, err := k.GetRepo(ctx, id)
	if err != nil {
		return err
	}
	active, err := k.ListCheckouts(ctx, CheckoutFilter{RepoID: id, Status: CheckoutActive})
	if err != nil {
		return err
	}
	if len(active) > 0 {
		return ErrRepoInUse
	}
	if _, err := k.DB.ExecContext(ctx, `DELETE FROM repos WHERE id = ?`, id); err != nil {
		return err
	}
	_, err = k.Append(ctx, EventInput{Source: source, Kind: "repo.forgotten", Payload: Payload{"repoId": r.ID, "name": r.Name, "path": r.Path}})
	return err
}

const checkoutCols = `id, work_item_id, repo_id, mode, path, branch, base, status, setup, created_at, updated_at`

func scanCheckout(s scanner) (Checkout, error) {
	var c Checkout
	var branch, base sql.NullString
	err := s.Scan(&c.ID, &c.WorkItemID, &c.RepoID, &c.Mode, &c.Path, &branch, &base, &c.Status, &c.Setup, &c.CreatedAt, &c.UpdatedAt)
	c.Branch, c.Base = str(branch), str(base)
	return c, err
}

type CheckoutFilter struct {
	WorkItemID string
	RepoID     string
	Status     string
}

// ListCheckouts lists checkouts, oldest first.
func (k *Kernel) ListCheckouts(ctx context.Context, f CheckoutFilter) ([]Checkout, error) {
	var where []string
	var args []any
	if f.WorkItemID != "" {
		where, args = append(where, "work_item_id = ?"), append(args, f.WorkItemID)
	}
	if f.RepoID != "" {
		where, args = append(where, "repo_id = ?"), append(args, f.RepoID)
	}
	if f.Status != "" {
		where, args = append(where, "status = ?"), append(args, f.Status)
	}
	q := `SELECT ` + checkoutCols + ` FROM checkouts`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := k.DB.QueryContext(ctx, q+` ORDER BY created_at ASC, rowid ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkout{}
	for rows.Next() {
		c, err := scanCheckout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (k *Kernel) GetCheckout(ctx context.Context, id string) (Checkout, error) {
	c, err := scanCheckout(k.DB.QueryRowContext(ctx, `SELECT `+checkoutCols+` FROM checkouts WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return c, fmt.Errorf("checkout not found: %s: %w", id, ErrNotFound)
	}
	return c, err
}

// CreateCheckout records a checkout once it exists on disk. An in-place
// checkout takes the repo's lease: one writer per original directory.
func (k *Kernel) CreateCheckout(ctx context.Context, c Checkout, repoName, source string) (Checkout, error) {
	item, err := k.GetWorkItem(ctx, c.WorkItemID)
	if err != nil {
		return c, err
	}
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	if c.Mode == CheckoutInPlace {
		var holder string
		err := tx.QueryRowContext(ctx, `SELECT work_item_id FROM checkouts WHERE repo_id = ? AND mode = ? AND status = ?`,
			c.RepoID, CheckoutInPlace, CheckoutActive).Scan(&holder)
		if err == nil {
			return c, &LeaseError{Holder: holder}
		} else if err != sql.ErrNoRows {
			return c, err
		}
	}
	now := k.stamp()
	c.ID = GenID("co", 6)
	c.Status = CheckoutActive
	if c.Setup == "" {
		c.Setup = SetupNone
	}
	c.CreatedAt, c.UpdatedAt = now, now
	if _, err := tx.ExecContext(ctx, `INSERT INTO checkouts (`+checkoutCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.WorkItemID, c.RepoID, c.Mode, c.Path, nullable(c.Branch), nullable(c.Base), c.Status, c.Setup, now, now); err != nil {
		return c, err
	}
	ev, err := k.appendTx(ctx, tx, EventInput{Source: source, Kind: "checkout.created", ThreadID: item.ThreadID, WorkItemID: item.ID,
		Payload: Payload{"checkoutId": c.ID, "repoId": c.RepoID, "repo": repoName, "mode": c.Mode, "path": c.Path,
			"branch": nullable(c.Branch), "base": nullable(c.Base)}})
	if err != nil {
		return c, err
	}
	if err := tx.Commit(); err != nil {
		return c, err
	}
	k.Hub.Publish(ev.Seq)
	return c, nil
}

// SetCheckoutSetup tracks the setup script's progress; its run is recorded as
// a side effect by whoever runs it.
func (k *Kernel) SetCheckoutSetup(ctx context.Context, id, state string) error {
	_, err := k.DB.ExecContext(ctx, `UPDATE checkouts SET setup = ?, updated_at = ? WHERE id = ?`, state, k.stamp(), id)
	return err
}

// ResumeInterruptedSetups puts back a setup the last runtime died in the
// middle of, so the next worker runs it again rather than waiting on it forever.
func (k *Kernel) ResumeInterruptedSetups(ctx context.Context) (int64, error) {
	res, err := k.DB.ExecContext(ctx, `UPDATE checkouts SET setup = ?, updated_at = ? WHERE setup = ? AND status = ?`,
		SetupPending, k.stamp(), SetupRunning, CheckoutActive)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ArchiveCheckout records that a checkout is gone from disk (or, in place,
// that its lease is released). The branch it worked on stays in the repo.
func (k *Kernel) ArchiveCheckout(ctx context.Context, id, source string, detail Payload) (Checkout, error) {
	c, err := k.GetCheckout(ctx, id)
	if err != nil || c.Status == CheckoutArchived {
		return c, err
	}
	item, err := k.GetWorkItem(ctx, c.WorkItemID)
	if err != nil {
		return c, err
	}
	if _, err := k.DB.ExecContext(ctx, `UPDATE checkouts SET status = ?, updated_at = ? WHERE id = ?`, CheckoutArchived, k.stamp(), id); err != nil {
		return c, err
	}
	payload := Payload{"checkoutId": c.ID, "repoId": c.RepoID, "mode": c.Mode, "path": c.Path, "branch": nullable(c.Branch)}
	maps.Copy(payload, detail)
	if _, err := k.Append(ctx, EventInput{Source: source, Kind: "checkout.archived", ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: payload}); err != nil {
		return c, err
	}
	return k.GetCheckout(ctx, id)
}
