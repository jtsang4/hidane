package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Lane is the delivery priority of a mailbox message: `interrupt` (a person
// talking) is taken before `normal`; idle work runs only when nothing is pending.
type Lane = string

const (
	LaneInterrupt Lane = "interrupt"
	LaneNormal    Lane = "normal"
)

// Payload is an event's JSON object.
type Payload map[string]any

// Str returns a string field, or "".
func (p Payload) Str(key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

// Bool reports whether a field is literally true.
func (p Payload) Bool(key string) bool {
	v, ok := p[key].(bool)
	return ok && v
}

// Num returns a numeric field (JSON numbers decode as float64).
func (p Payload) Num(key string) (float64, bool) {
	switch v := p[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// Event is one fact in the append-only log.
type Event struct {
	Seq         int64
	ID          string
	TS          string
	Source      string
	Kind        string
	ThreadID    string
	WorkItemID  string
	ExecutionID string
	Payload     Payload
	Mailbox     string
	Lane        string
	CausedBy    string
	Hop         int
}

type eventJSON struct {
	Seq         int64   `json:"seq"`
	ID          string  `json:"id"`
	TS          string  `json:"ts"`
	Source      string  `json:"source"`
	Kind        string  `json:"kind"`
	ThreadID    *string `json:"threadId"`
	WorkItemID  *string `json:"workItemId"`
	ExecutionID *string `json:"executionId"`
	Payload     Payload `json:"payload"`
	Mailbox     *string `json:"mailbox"`
	Lane        *string `json:"lane"`
	CausedBy    *string `json:"causedBy"`
	Hop         int     `json:"hop"`
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// MarshalJSON keeps the wire shape the web UI was built against: absent ids are null.
func (e Event) MarshalJSON() ([]byte, error) {
	p := e.Payload
	if p == nil {
		p = Payload{}
	}
	return json.Marshal(eventJSON{
		Seq: e.Seq, ID: e.ID, TS: e.TS, Source: e.Source, Kind: e.Kind,
		ThreadID: ptr(e.ThreadID), WorkItemID: ptr(e.WorkItemID), ExecutionID: ptr(e.ExecutionID),
		Payload: p, Mailbox: ptr(e.Mailbox), Lane: ptr(e.Lane), CausedBy: ptr(e.CausedBy), Hop: e.Hop,
	})
}

func (e *Event) UnmarshalJSON(b []byte) error {
	var j eventJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*e = Event{
		Seq: j.Seq, ID: j.ID, TS: j.TS, Source: j.Source, Kind: j.Kind,
		ThreadID: deref(j.ThreadID), WorkItemID: deref(j.WorkItemID), ExecutionID: deref(j.ExecutionID),
		Payload: j.Payload, Mailbox: deref(j.Mailbox), Lane: deref(j.Lane), CausedBy: deref(j.CausedBy), Hop: j.Hop,
	}
	return nil
}

// EventInput is what a writer supplies; the kernel assigns seq, id and ts.
type EventInput struct {
	Source      string
	Kind        string
	ThreadID    string
	WorkItemID  string
	ExecutionID string
	Payload     Payload
	// Mailbox makes the event also a message to that agent loop.
	Mailbox  string
	Lane     Lane
	CausedBy string
	Hop      int
}

// RedactedSQL is true for a person's message they chose to hide, or a
// Manager's forwarded copy of one. Evaluated against a row named `events`.
const RedactedSQL = `(events.kind = 'user.message' AND EXISTS (
  SELECT 1 FROM events r WHERE r.kind = 'message.redacted'
    AND json_extract(r.payload, '$.of') IN (events.id, json_extract(events.payload, '$.of'))))`

// Hiding is a read-time projection: the row is never rewritten (the log is
// append-only), but no reader sees the content again. Structural keys (root,
// of, target, channel) survive so threads still group.
const payloadSQL = `CASE WHEN ` + RedactedSQL + `
  THEN json_set(json_remove(events.payload, '$.text', '$.images', '$.imageCount'), '$.redacted', json('true'), '$.text', '')
  ELSE events.payload END AS payload`

// SelectCols reads an event row with masking applied.
const SelectCols = `events.seq, events.id, events.ts, events.source, events.kind, events.thread_id,
  events.work_item_id, events.execution_id, ` + payloadSQL + `, events.mailbox, events.lane, events.caused_by, events.hop`

type scanner interface{ Scan(dest ...any) error }

func scanEvent(s scanner) (Event, error) {
	var e Event
	var thread, item, exec, mailbox, lane, caused sql.NullString
	var payload string
	if err := s.Scan(&e.Seq, &e.ID, &e.TS, &e.Source, &e.Kind, &thread, &item, &exec, &payload,
		&mailbox, &lane, &caused, &e.Hop); err != nil {
		return e, err
	}
	e.ThreadID, e.WorkItemID, e.ExecutionID = str(thread), str(item), str(exec)
	e.Mailbox, e.Lane, e.CausedBy = str(mailbox), str(lane), str(caused)
	e.Payload = Payload{}
	if payload != "" {
		if err := json.Unmarshal([]byte(payload), &e.Payload); err != nil {
			return e, fmt.Errorf("event %s payload: %w", e.ID, err)
		}
	}
	return e, nil
}

func encodePayload(p Payload) (string, error) {
	if p == nil {
		return "{}", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Append writes one event (write-through, facts only) and wakes listeners.
func (k *Kernel) Append(ctx context.Context, in EventInput) (Event, error) {
	return k.appendTx(ctx, k.DB, in)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (k *Kernel) appendTx(ctx context.Context, db execer, in EventInput) (Event, error) {
	payload, err := encodePayload(in.Payload)
	if err != nil {
		return Event{}, err
	}
	lane := ""
	if in.Mailbox != "" {
		lane = in.Lane
		if lane == "" {
			lane = LaneNormal
		}
	}
	id := GenID("ev", 10)
	ts := k.stamp()
	res, err := db.ExecContext(ctx, `
		INSERT INTO events (id, ts, source, kind, thread_id, work_item_id, execution_id, payload,
		                    mailbox, lane, caused_by, hop)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ts, in.Source, in.Kind, nullable(in.ThreadID), nullable(in.WorkItemID), nullable(in.ExecutionID),
		payload, nullable(in.Mailbox), nullable(lane), nullable(in.CausedBy), in.Hop)
	if err != nil {
		return Event{}, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return Event{}, err
	}
	var p Payload
	_ = json.Unmarshal([]byte(payload), &p)
	if p == nil {
		p = Payload{}
	}
	ev := Event{
		Seq: seq, ID: id, TS: ts, Source: in.Source, Kind: in.Kind, ThreadID: in.ThreadID,
		WorkItemID: in.WorkItemID, ExecutionID: in.ExecutionID, Payload: p, Mailbox: in.Mailbox,
		Lane: lane, CausedBy: in.CausedBy, Hop: in.Hop,
	}
	if _, inTx := db.(*sql.Tx); !inTx {
		k.Hub.Publish(seq)
	}
	return ev, nil
}

// GetEvent returns one event by id (masked), or ok=false.
func (k *Kernel) GetEvent(ctx context.Context, id string) (Event, bool, error) {
	row := k.DB.QueryRowContext(ctx, `SELECT `+SelectCols+` FROM events WHERE id = ?`, id)
	e, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	return e, true, nil
}

// ListFilter selects events; zero values mean "no constraint".
type ListFilter struct {
	ThreadID   string
	WorkItemID string
	Mailbox    string
	Kind       string
	// Kinds are OR'd. Paging a chat needs this: filtering after the fetch gives
	// pages with an unpredictable number of renderable rows.
	Kinds       []string
	ExecutionID string
	AfterSeq    *int64
	BeforeSeq   *int64
	// Day is `YYYY-MM-DD` in the local timezone.
	Day string
	// PayloadEquals matches a top-level payload key: some things are identified
	// only inside the payload (a schedule's firings), and a tail-window scan
	// would silently lose history older than the window.
	PayloadKey   string
	PayloadValue string
	// Conversation: everything said on the main thread plus every answer,
	// whichever thread it was written on.
	Conversation bool
	// PersonOnly (with Conversation) drops answers to webhooks and schedules.
	PersonOnly bool
	Tail       int
	Limit      int
}

// ConversationMainKinds are rendered from the main thread.
var ConversationMainKinds = []string{
	"user.message", "agent.reply", "agent.error", "escalation",
	"message.attributed", "attribution.ambiguous", "message.redacted",
}

// ConversationAnyKinds are rendered whichever thread they were written on.
var ConversationAnyKinds = []string{"agent.reply", "execution.steered"}

func quoteList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "'" + x + "'"
	}
	return strings.Join(q, ", ")
}

// ConversationSQL is the conversation predicate.
var ConversationSQL = fmt.Sprintf(`((events.thread_id = 'main' AND events.kind IN (%s)) OR events.kind IN (%s))`,
	quoteList(ConversationMainKinds), quoteList(ConversationAnyKinds))

func Int64(v int64) *int64 { return &v }

// DayBounds converts a local `YYYY-MM-DD` into the UTC [start, end) stamps.
func DayBounds(day string) (string, string, error) {
	start, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return "", "", err
	}
	end := start.AddDate(0, 0, 1)
	return FormatTime(start), FormatTime(end), nil
}

// ListEvents reads events in seq order.
func (k *Kernel) ListEvents(ctx context.Context, f ListFilter) ([]Event, error) {
	var where []string
	var args []any
	add := func(clause string, v any) {
		where = append(where, clause)
		args = append(args, v)
	}
	if f.ThreadID != "" {
		add("events.thread_id = ?", f.ThreadID)
	}
	if f.WorkItemID != "" {
		add("events.work_item_id = ?", f.WorkItemID)
	}
	if f.Mailbox != "" {
		add("events.mailbox = ?", f.Mailbox)
	}
	if f.ExecutionID != "" {
		add("events.execution_id = ?", f.ExecutionID)
	}
	if f.Kind != "" {
		add("events.kind = ?", f.Kind)
	}
	if len(f.Kinds) > 0 {
		holes := make([]string, len(f.Kinds))
		for i, kind := range f.Kinds {
			holes[i] = "?"
			args = append(args, kind)
		}
		where = append(where, "events.kind IN ("+strings.Join(holes, ", ")+")")
	}
	if f.Conversation {
		where = append(where, ConversationSQL)
	}
	if f.PersonOnly {
		where = append(where, `coalesce(json_extract(events.payload, '$.rootKind'), '') NOT IN ('external', 'scheduled')`)
	}
	if f.AfterSeq != nil {
		add("events.seq > ?", *f.AfterSeq)
	}
	if f.BeforeSeq != nil {
		add("events.seq < ?", *f.BeforeSeq)
	}
	if f.PayloadKey != "" {
		where = append(where, "json_extract(events.payload, ?) = ?")
		args = append(args, "$."+f.PayloadKey, f.PayloadValue)
	}
	if f.Day != "" {
		start, end, err := DayBounds(f.Day)
		if err != nil {
			return nil, err
		}
		add("events.ts >= ?", start)
		add("events.ts < ?", end)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}
	var q string
	if f.Tail > 0 {
		q = fmt.Sprintf(`SELECT %s FROM events %s ORDER BY events.seq DESC LIMIT %d`, SelectCols, whereSQL, f.Tail)
	} else {
		limit := ""
		if f.Limit > 0 {
			limit = fmt.Sprintf("LIMIT %d", f.Limit)
		}
		q = fmt.Sprintf(`SELECT %s FROM events %s ORDER BY events.seq ASC %s`, SelectCols, whereSQL, limit)
	}
	rows, err := k.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if f.Tail > 0 {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, nil
}

// LatestSeq is the newest seq in the log (0 when empty).
func (k *Kernel) LatestSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	err := k.DB.QueryRowContext(ctx, `SELECT max(seq) FROM events`).Scan(&seq)
	return seq.Int64, err
}

// GetCursor returns a consumer's committed position (0 when absent).
func (k *Kernel) GetCursor(ctx context.Context, consumer string) (int64, error) {
	var seq int64
	err := k.DB.QueryRowContext(ctx, `SELECT seq FROM cursors WHERE consumer = ?`, consumer).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return seq, err
}

// CommitCursor moves a consumer's position.
func (k *Kernel) CommitCursor(ctx context.Context, consumer string, seq int64) error {
	_, err := k.DB.ExecContext(ctx, `
		INSERT INTO cursors (consumer, seq, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (consumer) DO UPDATE SET seq = excluded.seq, updated_at = excluded.updated_at`,
		consumer, seq, k.stamp())
	return err
}

// ResetCursor is how a projection or consumer replays: history never changes.
func (k *Kernel) ResetCursor(ctx context.Context, consumer string, seq int64) error {
	return k.CommitCursor(ctx, consumer, seq)
}

// NextBatch is the next uncommitted batch for a consumer; the caller commits.
func (k *Kernel) NextBatch(ctx context.Context, consumer string, limit int) ([]Event, error) {
	after, err := k.GetCursor(ctx, consumer)
	if err != nil {
		return nil, err
	}
	return k.ListEvents(ctx, ListFilter{AfterSeq: &after, Limit: limit})
}

// ScanEvent reads one row selected with SelectCols.
func ScanEvent(s interface{ Scan(dest ...any) error }) (Event, error) { return scanEvent(s) }
