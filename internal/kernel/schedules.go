package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/jtsang4/hidane/internal/clip"
)

// Schedules are user-defined timer connectors. Definitions live in a small
// state table (the scheduler needs an atomic due-query, like cursors); every
// definition change and every firing is recorded to the log.
//
//	http   — fetch a URL and CAPTURE the response as connector.http. Whether a
//	         model is woken is a declared triage hint (`wake`), decided at triage.
//	prompt — post text to the Primary, exactly like a person's message would.
type ScheduleSpec struct {
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Wake    bool              `json:"wake,omitempty"`
	Prompt  string            `json:"prompt,omitempty"`
}

type Schedule struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Action      string       `json:"action"`
	Spec        ScheduleSpec `json:"spec"`
	Cron        *string      `json:"cron"`
	IntervalSec *int         `json:"intervalSec"`
	Timezone    *string      `json:"timezone"`
	Enabled     bool         `json:"enabled"`
	NextRunAt   *string      `json:"nextRunAt"`
	LastRunAt   *string      `json:"lastRunAt"`
	LastStatus  *string      `json:"lastStatus"`
	CreatedAt   string       `json:"createdAt"`
	UpdatedAt   string       `json:"updatedAt"`
}

// ScheduleInput is a definition (create) or a patch (update; nil = keep).
type ScheduleInput struct {
	Name        *string       `json:"name"`
	Action      *string       `json:"action"`
	Spec        *ScheduleSpec `json:"spec"`
	Cron        *string       `json:"cron"`
	IntervalSec *int          `json:"intervalSec"`
	Timezone    *string       `json:"timezone"`
	Enabled     *bool         `json:"enabled"`
	// Present records which timing keys a patch mentioned (even as null), so
	// switching between cron and interval is a single PATCH.
	Present map[string]bool `json:"-"`
}

// UnmarshalJSON records which keys were present.
func (in *ScheduleInput) UnmarshalJSON(b []byte) error {
	type plain ScheduleInput
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return err
	}
	*in = ScheduleInput(p)
	in.Present = map[string]bool{}
	for k := range keys {
		in.Present[k] = true
	}
	return nil
}

var cronParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ComputeNextRun is the next firing time. Errors on invalid definitions so the
// API can reject them at definition time.
func ComputeNextRun(cronExpr string, intervalSec int, timezone string, from time.Time) (time.Time, error) {
	if cronExpr != "" {
		loc := time.Local
		if timezone != "" {
			l, err := time.LoadLocation(timezone)
			if err != nil {
				return time.Time{}, fmt.Errorf("unknown timezone: %s", timezone)
			}
			loc = l
		}
		sched, err := cronParser.Parse(cronExpr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid cron expression: %v", err)
		}
		next := sched.Next(from.In(loc))
		if next.IsZero() {
			return time.Time{}, fmt.Errorf("cron expression never fires: %s", cronExpr)
		}
		return next, nil
	}
	if intervalSec < 10 {
		return time.Time{}, fmt.Errorf("intervalSec must be a number >= 10")
	}
	return from.Add(time.Duration(intervalSec) * time.Second), nil
}

type scheduleDef struct {
	Name, Action, Cron, Timezone string
	Spec                         ScheduleSpec
	IntervalSec                  int
	Enabled                      bool
}

var httpURL = regexp.MustCompile(`^https?://`)

func validateDef(d scheduleDef) string {
	if strings.TrimSpace(d.Name) == "" {
		return "name required"
	}
	if d.Action != "http" && d.Action != "prompt" {
		return "action must be http | prompt"
	}
	if (d.Cron != "") == (d.IntervalSec != 0) {
		return "exactly one of cron / intervalSec is required"
	}
	if d.Action == "http" && !httpURL.MatchString(d.Spec.URL) {
		return "spec.url must be an http(s) URL"
	}
	if d.Action == "prompt" && strings.TrimSpace(d.Spec.Prompt) == "" {
		return "spec.prompt required"
	}
	if _, err := ComputeNextRun(d.Cron, d.IntervalSec, d.Timezone, time.Now()); err != nil {
		return err.Error()
	}
	return ""
}

// ValidationError is a definition mistake to surface to the person.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func iv(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

const scheduleCols = `id, name, action, spec, cron, interval_sec, timezone, enabled, next_run_at, last_run_at, last_status, created_at, updated_at`

func scanSchedule(s scanner) (Schedule, error) {
	var sc Schedule
	var spec string
	var cronS, tz, next, last, status sql.NullString
	var interval sql.NullInt64
	var enabled int
	if err := s.Scan(&sc.ID, &sc.Name, &sc.Action, &spec, &cronS, &interval, &tz, &enabled, &next, &last, &status, &sc.CreatedAt, &sc.UpdatedAt); err != nil {
		return sc, err
	}
	_ = json.Unmarshal([]byte(spec), &sc.Spec)
	sc.Cron, sc.Timezone = ptr(str(cronS)), ptr(str(tz))
	sc.NextRunAt, sc.LastRunAt, sc.LastStatus = ptr(str(next)), ptr(str(last)), ptr(str(status))
	if interval.Valid {
		n := int(interval.Int64)
		sc.IntervalSec = &n
	}
	sc.Enabled = enabled == 1
	return sc, nil
}

func (k *Kernel) CreateSchedule(ctx context.Context, in ScheduleInput, source string) (Schedule, error) {
	d := scheduleDef{Name: deref(in.Name), Action: deref(in.Action), Cron: deref(in.Cron), Timezone: deref(in.Timezone), IntervalSec: iv(in.IntervalSec), Enabled: true}
	if in.Spec != nil {
		d.Spec = *in.Spec
	}
	if in.Enabled != nil {
		d.Enabled = *in.Enabled
	}
	if msg := validateDef(d); msg != "" {
		return Schedule{}, ValidationError{msg}
	}
	next, _ := ComputeNextRun(d.Cron, d.IntervalSec, d.Timezone, k.Now())
	id := GenID("sc", 6)
	spec, _ := json.Marshal(d.Spec)
	now := k.stamp()
	var interval any
	if d.IntervalSec != 0 {
		interval = d.IntervalSec
	}
	if _, err := k.DB.ExecContext(ctx, `INSERT INTO schedules (id, name, action, spec, cron, interval_sec, timezone, enabled, next_run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, strings.TrimSpace(d.Name), d.Action, string(spec), nullable(d.Cron), interval, nullable(d.Timezone), boolInt(d.Enabled), FormatTime(next), now, now); err != nil {
		return Schedule{}, err
	}
	sc, err := k.GetSchedule(ctx, id)
	if err != nil {
		return sc, err
	}
	_, err = k.Append(ctx, EventInput{Source: source, Kind: "schedule.created", Payload: Payload{
		"scheduleId": id, "name": sc.Name, "action": sc.Action, "cron": sc.Cron, "intervalSec": sc.IntervalSec,
	}})
	return sc, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (k *Kernel) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	sc, err := scanSchedule(k.DB.QueryRowContext(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return sc, fmt.Errorf("schedule not found: %s: %w", id, ErrNotFound)
	}
	return sc, err
}

func (k *Kernel) querySchedules(ctx context.Context, q string, args ...any) ([]Schedule, error) {
	rows, err := k.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (k *Kernel) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return k.querySchedules(ctx, `SELECT `+scheduleCols+` FROM schedules ORDER BY created_at ASC, rowid ASC`)
}

func (k *Kernel) UpdateSchedule(ctx context.Context, id string, patch ScheduleInput, source string) (Schedule, error) {
	cur, err := k.GetSchedule(ctx, id)
	if err != nil {
		return cur, err
	}
	d := scheduleDef{Name: cur.Name, Action: cur.Action, Spec: cur.Spec, Cron: deref(cur.Cron), IntervalSec: iv(cur.IntervalSec), Timezone: deref(cur.Timezone), Enabled: cur.Enabled}
	if patch.Name != nil {
		d.Name = *patch.Name
	}
	if patch.Action != nil {
		d.Action = *patch.Action
	}
	if patch.Spec != nil {
		d.Spec = *patch.Spec
	}
	if patch.Present["cron"] || patch.Cron != nil {
		d.Cron = deref(patch.Cron)
	}
	if patch.Present["intervalSec"] || patch.IntervalSec != nil {
		d.IntervalSec = iv(patch.IntervalSec)
	}
	if patch.Present["timezone"] || patch.Timezone != nil {
		d.Timezone = deref(patch.Timezone)
	}
	if patch.Enabled != nil {
		d.Enabled = *patch.Enabled
	}
	if msg := validateDef(d); msg != "" {
		return cur, ValidationError{msg}
	}
	var next any
	if d.Enabled {
		t, _ := ComputeNextRun(d.Cron, d.IntervalSec, d.Timezone, k.Now())
		next = FormatTime(t)
	}
	var interval any
	if d.IntervalSec != 0 {
		interval = d.IntervalSec
	}
	spec, _ := json.Marshal(d.Spec)
	if _, err := k.DB.ExecContext(ctx, `UPDATE schedules SET name = ?, action = ?, spec = ?, cron = ?, interval_sec = ?, timezone = ?,
		enabled = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(d.Name), d.Action, string(spec), nullable(d.Cron), interval, nullable(d.Timezone), boolInt(d.Enabled), next, k.stamp(), id); err != nil {
		return cur, err
	}
	sc, err := k.GetSchedule(ctx, id)
	if err != nil {
		return sc, err
	}
	_, err = k.Append(ctx, EventInput{Source: source, Kind: "schedule.updated", Payload: Payload{"scheduleId": id, "enabled": sc.Enabled, "name": sc.Name}})
	return sc, err
}

func (k *Kernel) DeleteSchedule(ctx context.Context, id, source string) error {
	sc, err := k.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if _, err := k.DB.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id); err != nil {
		return err
	}
	_, err = k.Append(ctx, EventInput{Source: source, Kind: "schedule.deleted", Payload: Payload{"scheduleId": id, "name": sc.Name}})
	return err
}

// DueSchedules are enabled schedules whose time has come.
func (k *Kernel) DueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	return k.querySchedules(ctx, `SELECT `+scheduleCols+` FROM schedules
		WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ? ORDER BY next_run_at ASC`, FormatTime(now))
}

// NextAfterRun is when a schedule should next run, given when it was *due*.
//
// Computing from the firing time instead drifts: a firing lands slightly after
// the tick that triggered it, so the next due time lands just after the
// following tick, which skips it — a 15s schedule was measured firing every
// 30s. Anchoring to the due time keeps the cadence on-grid; missed periods are
// skipped rather than replayed, so downtime costs one firing, not a storm.
func NextAfterRun(sc Schedule, now time.Time) time.Time {
	if sc.Cron != nil && *sc.Cron != "" {
		t, err := ComputeNextRun(*sc.Cron, 0, deref(sc.Timezone), now)
		if err == nil {
			return t
		}
	}
	interval := time.Duration(iv(sc.IntervalSec)) * time.Second
	if interval <= 0 {
		return now.Add(time.Hour)
	}
	anchor := now
	if sc.NextRunAt != nil {
		if t, err := ParseTime(*sc.NextRunAt); err == nil {
			anchor = t
		}
	}
	next := anchor.Add(interval)
	if !next.After(now) {
		periods := math.Ceil(float64(now.Sub(anchor)) / float64(interval))
		next = anchor.Add(time.Duration(periods) * interval)
		if !next.After(now) {
			next = next.Add(interval)
		}
	}
	return next
}

// MarkRun does the bookkeeping after a firing. A firing before the schedule
// was due (run now) leaves its clock alone: a manual test must not postpone
// the real run.
func (k *Kernel) MarkRun(ctx context.Context, id, status string, now time.Time) error {
	sc, err := k.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	var next any
	if sc.Enabled {
		next = FormatTime(NextAfterRun(sc, now))
		if sc.NextRunAt != nil {
			if due, err := ParseTime(*sc.NextRunAt); err == nil && due.After(now) {
				next = *sc.NextRunAt
			}
		}
	}
	status = clip.Runes(status, 200)
	_, err = k.DB.ExecContext(ctx, `UPDATE schedules SET last_run_at = ?, last_status = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		FormatTime(now), status, next, k.stamp(), id)
	return err
}
