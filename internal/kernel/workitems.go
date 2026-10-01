package kernel

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type WorkItemStatus = string

const (
	StatusOpen   WorkItemStatus = "open"
	StatusDone   WorkItemStatus = "done"
	StatusClosed WorkItemStatus = "closed"
)

func ValidStatus(s string) bool { return s == StatusOpen || s == StatusDone || s == StatusClosed }

// WorkItem: the thread is the interaction lane, the workspace the execution home.
type WorkItem struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Workspace  string  `json:"workspace"`
	ThreadID   string  `json:"threadId"`
	ParentID   *string `json:"parentId"`
	DeadlineAt *string `json:"deadlineAt"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

func (w WorkItem) Parent() string { return deref(w.ParentID) }

const wiCols = `id, title, status, workspace, thread_id, parent_id, deadline_at, created_at, updated_at`

func scanWorkItem(s scanner) (WorkItem, error) {
	var w WorkItem
	var parent, deadline sql.NullString
	err := s.Scan(&w.ID, &w.Title, &w.Status, &w.Workspace, &w.ThreadID, &parent, &deadline, &w.CreatedAt, &w.UpdatedAt)
	w.ParentID = ptr(str(parent))
	w.DeadlineAt = ptr(str(deadline))
	return w, err
}

type CreateWorkItemOpts struct {
	Repo     string
	ParentID string
	// Of is the message this work item was created for (its conversation anchor).
	Of string
}

// CreateWorkItem creates a work item with its thread and workspace and records the fact.
func (k *Kernel) CreateWorkItem(ctx context.Context, title, source string, opts CreateWorkItemOpts) (WorkItem, error) {
	id := GenID("wi", 6)
	threadID := GenID("th", 6)
	ws := k.EnsureWorkspace(id, opts.Repo)
	now := k.stamp()
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return WorkItem{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO threads (id, work_item_id, kind, created_at) VALUES (?, ?, 'work', ?)`,
		threadID, id, now); err != nil {
		return WorkItem{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_items (id, title, workspace, thread_id, parent_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, title, ws.Path, threadID, nullable(opts.ParentID), now, now); err != nil {
		return WorkItem{}, err
	}
	payload := Payload{
		"title":          title,
		"workspace":      ws.Path,
		"provider":       ws.Provider,
		"branch":         nilIfEmpty(ws.Branch),
		"repo":           nilIfEmpty(opts.Repo),
		"workspaceError": nilIfEmpty(ws.Error),
		"parentId":       nilIfEmpty(opts.ParentID),
	}
	if opts.Of != "" {
		payload["of"] = opts.Of
	}
	ev, err := k.appendTx(ctx, tx, EventInput{
		Source: source, Kind: "work_item.created", ThreadID: threadID, WorkItemID: id, Payload: payload,
	})
	if err != nil {
		return WorkItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkItem{}, err
	}
	k.Hub.Publish(ev.Seq)
	return k.GetWorkItem(ctx, id)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (k *Kernel) GetWorkItem(ctx context.Context, id string) (WorkItem, error) {
	w, err := scanWorkItem(k.DB.QueryRowContext(ctx, `SELECT `+wiCols+` FROM work_items WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return w, fmt.Errorf("work item not found: %s: %w", id, ErrNotFound)
	}
	return w, err
}

func (k *Kernel) queryWorkItems(ctx context.Context, q string, args ...any) ([]WorkItem, error) {
	rows, err := k.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkItem{}
	for rows.Next() {
		w, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListWorkItems lists all items, or those with one status, oldest first.
func (k *Kernel) ListWorkItems(ctx context.Context, status string) ([]WorkItem, error) {
	if status != "" {
		return k.queryWorkItems(ctx, `SELECT `+wiCols+` FROM work_items WHERE status = ? ORDER BY created_at ASC, rowid ASC`, status)
	}
	return k.queryWorkItems(ctx, `SELECT `+wiCols+` FROM work_items ORDER BY created_at ASC, rowid ASC`)
}

// SetWorkItemStatus records every transition as a fact.
func (k *Kernel) SetWorkItemStatus(ctx context.Context, id, status, source string) (WorkItem, error) {
	item, err := k.GetWorkItem(ctx, id)
	if err != nil {
		return item, err
	}
	if _, err := k.DB.ExecContext(ctx, `UPDATE work_items SET status = ?, updated_at = ? WHERE id = ?`, status, k.stamp(), id); err != nil {
		return item, err
	}
	if _, err := k.Append(ctx, EventInput{
		Source: source, Kind: "work_item.status_changed", ThreadID: item.ThreadID, WorkItemID: id,
		Payload: Payload{"from": item.Status, "to": status},
	}); err != nil {
		return item, err
	}
	return k.GetWorkItem(ctx, id)
}

func (k *Kernel) ListChildren(ctx context.Context, parentID string) ([]WorkItem, error) {
	return k.queryWorkItems(ctx, `SELECT `+wiCols+` FROM work_items WHERE parent_id = ? ORDER BY created_at ASC, rowid ASC`, parentID)
}

// Subtree is the item and every descendant, parents before children.
func (k *Kernel) Subtree(ctx context.Context, id string) ([]WorkItem, error) {
	return k.queryWorkItems(ctx, `
		WITH RECURSIVE tree(id, depth) AS (
			SELECT id, 0 FROM work_items WHERE id = ?
			UNION ALL
			SELECT w.id, tree.depth + 1 FROM work_items w JOIN tree ON w.parent_id = tree.id
		)
		SELECT `+prefixCols("w", wiCols)+` FROM tree JOIN work_items w ON w.id = tree.id
		ORDER BY tree.depth ASC, w.created_at ASC, w.rowid ASC`, id)
}

func prefixCols(alias, cols string) string {
	out := ""
	for i, c := range splitCols(cols) {
		if i > 0 {
			out += ", "
		}
		out += alias + "." + c
	}
	return out
}

func splitCols(cols string) []string {
	var out []string
	cur := ""
	for _, r := range cols {
		switch r {
		case ',':
			out = append(out, cur)
			cur = ""
		case ' ', '\n', '\t':
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// Ancestors from the parent up to the root.
func (k *Kernel) Ancestors(ctx context.Context, item WorkItem) ([]WorkItem, error) {
	var chain []WorkItem
	parent := item.Parent()
	for parent != "" && len(chain) < 32 {
		p, err := k.GetWorkItem(ctx, parent)
		if err != nil {
			return chain, err
		}
		chain = append(chain, p)
		parent = p.Parent()
	}
	return chain, nil
}

// SetWorkItemDeadline sets or clears (empty) a deadline.
func (k *Kernel) SetWorkItemDeadline(ctx context.Context, id, deadlineAt, source string) (WorkItem, error) {
	item, err := k.GetWorkItem(ctx, id)
	if err != nil {
		return item, err
	}
	var stored any
	if deadlineAt != "" {
		t, err := ParseTime(deadlineAt)
		if err != nil {
			return item, fmt.Errorf("deadlineAt must be an ISO timestamp: %w", err)
		}
		stored = FormatTime(t)
	}
	if _, err := k.DB.ExecContext(ctx, `UPDATE work_items SET deadline_at = ?, updated_at = ? WHERE id = ?`, stored, k.stamp(), id); err != nil {
		return item, err
	}
	if _, err := k.Append(ctx, EventInput{
		Source: source, Kind: "work_item.deadline_set", ThreadID: item.ThreadID, WorkItemID: id,
		Payload: Payload{"deadlineAt": stored},
	}); err != nil {
		return item, err
	}
	return k.GetWorkItem(ctx, id)
}

// OverdueWorkItems are open items whose deadline has passed.
func (k *Kernel) OverdueWorkItems(ctx context.Context, now time.Time) ([]WorkItem, error) {
	return k.queryWorkItems(ctx, `SELECT `+wiCols+` FROM work_items
		WHERE status = 'open' AND deadline_at IS NOT NULL AND deadline_at <= ?`, FormatTime(now))
}

// TitlesFor maps work item ids to titles.
func (k *Kernel) TitlesFor(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	holes := ""
	for i, id := range ids {
		args[i] = id
		if i > 0 {
			holes += ", "
		}
		holes += "?"
	}
	rows, err := k.DB.QueryContext(ctx, `SELECT id, title FROM work_items WHERE id IN (`+holes+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[id] = title
	}
	return out, rows.Err()
}
