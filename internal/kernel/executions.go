package kernel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// ExecutionStatus is the lifecycle of one worker execution.
type ExecutionStatus = string

const (
	ExecQueued    ExecutionStatus = "queued"
	ExecRunning   ExecutionStatus = "running"
	ExecDone      ExecutionStatus = "done"
	ExecFailed    ExecutionStatus = "failed"
	ExecCancelled ExecutionStatus = "cancelled"
	ExecLost      ExecutionStatus = "lost"
)

// Execution has an owner mailbox that receives its outcome — every execution
// has someone to read how it ended.
type Execution struct {
	ID         string  `json:"id"`
	WorkItemID string  `json:"workItemId"`
	Owner      string  `json:"owner"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"createdAt"`
	StartedAt  *string `json:"startedAt"`
	FinishedAt *string `json:"finishedAt"`
}

func scanExecution(s scanner) (Execution, error) {
	var e Execution
	var started, finished sql.NullString
	err := s.Scan(&e.ID, &e.WorkItemID, &e.Owner, &e.Status, &e.CreatedAt, &started, &finished)
	e.StartedAt = ptr(str(started))
	e.FinishedAt = ptr(str(finished))
	return e, err
}

const execCols = `id, work_item_id, owner, status, created_at, started_at, finished_at`

func (k *Kernel) CreateExecution(ctx context.Context, id, workItemID, owner string) error {
	_, err := k.DB.ExecContext(ctx,
		`INSERT INTO executions (id, work_item_id, owner, created_at) VALUES (?, ?, ?, ?)`,
		id, workItemID, owner, k.stamp())
	return err
}

func (k *Kernel) SetExecutionStatus(ctx context.Context, id string, status ExecutionStatus) error {
	var err error
	switch status {
	case ExecRunning:
		_, err = k.DB.ExecContext(ctx, `UPDATE executions SET status = ?, started_at = ? WHERE id = ?`, status, k.stamp(), id)
	default:
		_, err = k.DB.ExecContext(ctx, `UPDATE executions SET status = ?, finished_at = ? WHERE id = ?`, status, k.stamp(), id)
	}
	return err
}

func (k *Kernel) GetExecution(ctx context.Context, id string) (Execution, bool, error) {
	e, err := scanExecution(k.DB.QueryRowContext(ctx, `SELECT `+execCols+` FROM executions WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return e, false, nil
	}
	return e, err == nil, err
}

func (k *Kernel) queryExecutions(ctx context.Context, q string, args ...any) ([]Execution, error) {
	rows, err := k.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ActiveExecutions are queued or running, oldest first.
func (k *Kernel) ActiveExecutions(ctx context.Context) ([]Execution, error) {
	return k.queryExecutions(ctx, `SELECT `+execCols+` FROM executions
		WHERE status IN ('queued', 'running') ORDER BY created_at ASC, rowid ASC`)
}

func (k *Kernel) ActiveExecutionFor(ctx context.Context, workItemID string) (Execution, bool, error) {
	list, err := k.queryExecutions(ctx, `SELECT `+execCols+` FROM executions
		WHERE work_item_id = ? AND status IN ('queued', 'running') ORDER BY created_at ASC, rowid ASC LIMIT 1`, workItemID)
	if err != nil || len(list) == 0 {
		return Execution{}, false, err
	}
	return list[0], true, nil
}

// BusyWorkItemIDs are work items with a queued or running execution.
func (k *Kernel) BusyWorkItemIDs(ctx context.Context) ([]string, error) {
	rows, err := k.DB.QueryContext(ctx, `SELECT DISTINCT work_item_id FROM executions WHERE status IN ('queued', 'running')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CountExecutionsSinceInput counts a work item's executions since the last
// message from outside the item reached its mailbox (a person, or a parent).
func (k *Kernel) CountExecutionsSinceInput(ctx context.Context, workItemID, mailbox string) (int, error) {
	var since sql.NullString
	if err := k.DB.QueryRowContext(ctx, `SELECT max(ts) FROM events WHERE mailbox = ? AND kind = 'user.message'`, mailbox).Scan(&since); err != nil {
		return 0, err
	}
	var n int
	err := k.DB.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE work_item_id = ? AND created_at > ?`,
		workItemID, since.String).Scan(&n)
	return n, err
}

func (k *Kernel) CountExecutions(ctx context.Context, workItemID string) (int, error) {
	var n int
	err := k.DB.QueryRowContext(ctx, `SELECT count(*) FROM executions WHERE work_item_id = ?`, workItemID).Scan(&n)
	return n, err
}

// WorkspacePath: every work item owns exactly one workspace directory.
func (k *Kernel) WorkspacePath(workItemID string) string {
	return filepath.Join(k.Cfg.WorkspacesDir(), workItemID)
}

// EnsureWorkspace creates the work item's own directory. The repositories it
// works on are checkouts attached to it later (internal/repos), so the
// kernel never needs to know what carries them.
func (k *Kernel) EnsureWorkspace(workItemID string) string {
	dir := k.WorkspacePath(workItemID)
	_ = os.MkdirAll(filepath.Join(dir, ".hidane", "sessions"), 0o755)
	return dir
}

// ErrNotFound is returned for unknown ids.
var ErrNotFound = fmt.Errorf("not found")
