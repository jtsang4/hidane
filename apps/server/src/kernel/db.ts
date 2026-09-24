import postgres from "postgres";
import { config } from "../config.js";

export type Sql = ReturnType<typeof postgres>;

let sqlInstance: Sql | undefined;

export function sql(): Sql {
  sqlInstance ??= postgres(config.databaseUrl, {
    onnotice: () => {},
    // Every live browser tab holds an SSE stream that polls on a timer, so the
    // pool has to cover concurrent readers plus the agent lanes; 5 starved
    // under a handful of simultaneous connections.
    max: 20,
  });
  return sqlInstance;
}

/** Idempotent schema bootstrap. Safe to run on every start. */
export async function migrate(): Promise<void> {
  const db = sql();
  await db`
    CREATE TABLE IF NOT EXISTS events (
      seq BIGSERIAL PRIMARY KEY,
      id TEXT NOT NULL UNIQUE,
      ts TIMESTAMPTZ NOT NULL DEFAULT now(),
      source TEXT NOT NULL,
      kind TEXT NOT NULL,
      thread_id TEXT,
      work_item_id TEXT,
      execution_id TEXT,
      payload JSONB NOT NULL DEFAULT '{}'
    )`;
  await db`CREATE INDEX IF NOT EXISTS events_thread_idx ON events (thread_id, seq)`;
  await db`CREATE INDEX IF NOT EXISTS events_work_item_idx ON events (work_item_id, seq)`;
  await db`CREATE INDEX IF NOT EXISTS events_ts_idx ON events (ts)`;
  await db`
    CREATE TABLE IF NOT EXISTS cursors (
      consumer TEXT PRIMARY KEY,
      seq BIGINT NOT NULL DEFAULT 0,
      updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`;
  await db`
    CREATE TABLE IF NOT EXISTS threads (
      id TEXT PRIMARY KEY,
      work_item_id TEXT,
      kind TEXT NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`;
  await db`
    CREATE TABLE IF NOT EXISTS work_items (
      id TEXT PRIMARY KEY,
      title TEXT NOT NULL,
      status TEXT NOT NULL DEFAULT 'open',
      workspace TEXT NOT NULL,
      thread_id TEXT NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`;
  await db`
    CREATE TABLE IF NOT EXISTS channel_bindings (
      id TEXT PRIMARY KEY,
      channel TEXT NOT NULL,
      kind TEXT NOT NULL,
      work_item_id TEXT,
      chat_id TEXT NOT NULL,
      root_id TEXT,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`;
  await db`CREATE INDEX IF NOT EXISTS bindings_ref_idx ON channel_bindings (channel, chat_id, root_id)`;
  await db`
    CREATE TABLE IF NOT EXISTS schedules (
      id TEXT PRIMARY KEY,
      name TEXT NOT NULL,
      action TEXT NOT NULL,
      spec JSONB NOT NULL DEFAULT '{}',
      cron TEXT,
      interval_sec INT,
      timezone TEXT,
      enabled BOOLEAN NOT NULL DEFAULT true,
      next_run_at TIMESTAMPTZ,
      last_run_at TIMESTAMPTZ,
      last_status TEXT,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`;
  await db`CREATE INDEX IF NOT EXISTS schedules_due_idx ON schedules (enabled, next_run_at)`;
  await db`INSERT INTO threads (id, kind) VALUES ('main', 'main') ON CONFLICT DO NOTHING`;

  // Mailbox addressing: an event with a mailbox is also a message to that
  // agent loop. Old rows keep NULLs; nothing is back-filled.
  await db`ALTER TABLE events ADD COLUMN IF NOT EXISTS mailbox TEXT`;
  await db`ALTER TABLE events ADD COLUMN IF NOT EXISTS lane TEXT`;
  await db`ALTER TABLE events ADD COLUMN IF NOT EXISTS caused_by TEXT`;
  await db`ALTER TABLE events ADD COLUMN IF NOT EXISTS hop INT NOT NULL DEFAULT 0`;
  await db`CREATE INDEX IF NOT EXISTS events_mailbox_idx ON events (mailbox, seq) WHERE mailbox IS NOT NULL`;
  // Every append wakes listeners in any process (daemon runtime, SSE streams),
  // including appends made by a one-shot CLI process.
  await db.unsafe(`
    CREATE OR REPLACE FUNCTION hidane_notify_event() RETURNS trigger AS $$
    BEGIN
      PERFORM pg_notify('hidane_events', NEW.seq::text);
      RETURN NEW;
    END $$ LANGUAGE plpgsql`);
  await db.unsafe(`
    CREATE OR REPLACE TRIGGER events_notify AFTER INSERT ON events
    FOR EACH ROW EXECUTE FUNCTION hidane_notify_event()`);

  await db`ALTER TABLE work_items ADD COLUMN IF NOT EXISTS parent_id TEXT`;
  await db`ALTER TABLE work_items ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ`;
  await db`CREATE INDEX IF NOT EXISTS work_items_parent_idx ON work_items (parent_id)`;
  await db`
    CREATE TABLE IF NOT EXISTS executions (
      id TEXT PRIMARY KEY,
      work_item_id TEXT NOT NULL,
      owner TEXT NOT NULL,
      status TEXT NOT NULL DEFAULT 'queued',
      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      started_at TIMESTAMPTZ,
      finished_at TIMESTAMPTZ
    )`;
  await db`CREATE INDEX IF NOT EXISTS executions_status_idx ON executions (status, created_at)`;
  await db`CREATE INDEX IF NOT EXISTS executions_item_idx ON executions (work_item_id, created_at)`;
}

export async function closeDb(): Promise<void> {
  if (sqlInstance) {
    await sqlInstance.end({ timeout: 5 });
    sqlInstance = undefined;
  }
}
