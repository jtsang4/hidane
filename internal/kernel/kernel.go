package kernel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jtsang4/hidane/internal/config"
)

// Kernel is the domain-agnostic core: the append-only event log and the small
// state tables that need atomic, ordered multi-writer access. Everything an
// agent or a person reads is a file elsewhere under Home.
type Kernel struct {
	DB  *sql.DB
	Cfg *config.Config
	Hub *Hub
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// Open opens (creating if needed) the database under cfg.Home and migrates it.
func Open(cfg *config.Config) (*Kernel, error) {
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(cfg.DBPath()) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Several readers (SSE streams, turns) plus serialized writers; SQLite
	// queues writers itself under busy_timeout.
	db.SetMaxOpenConns(8)
	k := &Kernel{DB: db, Cfg: cfg, Hub: NewHub(), Now: time.Now}
	if err := k.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return k, nil
}

func (k *Kernel) Close() error { return k.DB.Close() }

// Ping proves the database answers (the /health probe).
func (k *Kernel) Ping(ctx context.Context) error {
	var one int
	return k.DB.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS events (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		id TEXT NOT NULL UNIQUE,
		ts TEXT NOT NULL,
		source TEXT NOT NULL,
		kind TEXT NOT NULL,
		thread_id TEXT,
		work_item_id TEXT,
		execution_id TEXT,
		payload TEXT NOT NULL DEFAULT '{}',
		mailbox TEXT,
		lane TEXT,
		caused_by TEXT,
		hop INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS events_thread_idx ON events (thread_id, seq)`,
	`CREATE INDEX IF NOT EXISTS events_work_item_idx ON events (work_item_id, seq)`,
	`CREATE INDEX IF NOT EXISTS events_ts_idx ON events (ts)`,
	`CREATE INDEX IF NOT EXISTS events_kind_idx ON events (kind, seq)`,
	`CREATE INDEX IF NOT EXISTS events_execution_idx ON events (execution_id, kind)`,
	`CREATE INDEX IF NOT EXISTS events_mailbox_idx ON events (mailbox, seq) WHERE mailbox IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS events_redacted_idx ON events (json_extract(payload, '$.of')) WHERE kind = 'message.redacted'`,
	// The log is append-only: enforced by the database, not just by convention.
	`CREATE TRIGGER IF NOT EXISTS events_no_update BEFORE UPDATE ON events
		BEGIN SELECT RAISE(ABORT, 'events are append-only'); END`,
	`CREATE TRIGGER IF NOT EXISTS events_no_delete BEFORE DELETE ON events
		BEGIN SELECT RAISE(ABORT, 'events are append-only'); END`,
	`CREATE TABLE IF NOT EXISTS cursors (
		consumer TEXT PRIMARY KEY,
		seq INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		work_item_id TEXT,
		kind TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS work_items (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		workspace TEXT NOT NULL,
		thread_id TEXT NOT NULL,
		parent_id TEXT,
		deadline_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS work_items_parent_idx ON work_items (parent_id)`,
	`CREATE TABLE IF NOT EXISTS executions (
		id TEXT PRIMARY KEY,
		work_item_id TEXT NOT NULL,
		owner TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'queued',
		created_at TEXT NOT NULL,
		started_at TEXT,
		finished_at TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS executions_status_idx ON executions (status, created_at)`,
	`CREATE INDEX IF NOT EXISTS executions_item_idx ON executions (work_item_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS channel_bindings (
		id TEXT PRIMARY KEY,
		channel TEXT NOT NULL,
		kind TEXT NOT NULL,
		work_item_id TEXT,
		chat_id TEXT NOT NULL,
		root_id TEXT,
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS bindings_ref_idx ON channel_bindings (channel, chat_id, root_id)`,
	`CREATE TABLE IF NOT EXISTS schedules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		action TEXT NOT NULL,
		spec TEXT NOT NULL DEFAULT '{}',
		cron TEXT,
		interval_sec INTEGER,
		timezone TEXT,
		enabled INTEGER NOT NULL DEFAULT 1,
		next_run_at TEXT,
		last_run_at TEXT,
		last_status TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS schedules_due_idx ON schedules (enabled, next_run_at)`,
}

// columns added after a table first shipped: created on databases that
// predate them (CREATE TABLE IF NOT EXISTS leaves an existing table as is).
var columns = []struct{ table, name, decl string }{
	{"work_items", "run_as", "TEXT"},
}

func (k *Kernel) migrate(ctx context.Context) error {
	for _, stmt := range schema {
		if _, err := k.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w\n%s", err, stmt)
		}
	}
	for _, c := range columns {
		var n int
		if err := k.DB.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := k.DB.ExecContext(ctx, `ALTER TABLE `+c.table+` ADD COLUMN `+c.name+` `+c.decl); err != nil {
				return err
			}
		}
	}
	_, err := k.DB.ExecContext(ctx,
		`INSERT INTO threads (id, kind, created_at) VALUES ('main', 'main', ?) ON CONFLICT DO NOTHING`,
		k.stamp())
	return err
}

// TimeLayout matches JavaScript's Date#toISOString, so timestamps sort as
// strings and the web UI parses them unchanged.
const TimeLayout = "2006-01-02T15:04:05.000Z"

func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

func ParseTime(s string) (time.Time, error) {
	if t, err := time.Parse(TimeLayout, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func (k *Kernel) stamp() string { return FormatTime(k.Now()) }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func str(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
