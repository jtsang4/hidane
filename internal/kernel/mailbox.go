package kernel

import (
	"context"
	"fmt"
	"strings"
)

// MailboxCursor names the cursor of an agent loop's mailbox. A mailbox is a
// derived view of the log, not a table: replaying it is resetting this cursor.
func MailboxCursor(address string) string { return "mailbox:" + address }

// RootOf is the main-thread message a chain of events started from.
func RootOf(e Event) string {
	if r := e.Payload.Str("root"); r != "" {
		return r
	}
	return e.ID
}

// PostInput delivers a message to an agent loop.
type PostInput struct {
	EventInput
	// CausedBy drives the causal hop budget.
	CausedBy *Event
}

// Post delivers a message by appending it. It returns ok=false when the causal
// hop budget is spent: the chain stops and a person is told instead, so two
// agents that keep waking each other cost a bounded amount.
func (k *Kernel) Post(ctx context.Context, in PostInput) (Event, bool, error) {
	if in.Mailbox == "" {
		return Event{}, false, fmt.Errorf("post without a mailbox")
	}
	hop := 0
	causedBy := ""
	if in.CausedBy != nil {
		hop = in.CausedBy.Hop + 1
		causedBy = in.CausedBy.ID
	}
	if hop > k.Cfg.MaxHops {
		payload := Payload{
			"reason":      "budget",
			"question":    fmt.Sprintf("这条因果链已经连续触发 %d 次，已自动暂停。需要继续的话，直接回复这个任务。", hop),
			"blockedKind": in.Kind,
			"mailbox":     in.Mailbox,
		}
		if in.CausedBy != nil {
			payload["root"] = RootOf(*in.CausedBy)
		}
		_, err := k.Append(ctx, EventInput{
			Source: "kernel:runtime", Kind: "escalation", ThreadID: "main",
			WorkItemID: in.WorkItemID, CausedBy: causedBy, Hop: hop, Payload: payload,
		})
		return Event{}, false, err
	}
	ev := in.EventInput
	ev.CausedBy = causedBy
	ev.Hop = hop
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, false, err
	}
	defer tx.Rollback()
	appended, err := k.appendTx(ctx, tx, ev)
	if err != nil {
		return Event{}, false, err
	}
	// Seed the cursor from the oldest message rather than this one: a
	// concurrent first post with a higher seq must not be able to skip ours.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cursors (consumer, seq, updated_at)
		SELECT ?, COALESCE(MIN(seq), ?) - 1, ? FROM events WHERE mailbox = ?
		ON CONFLICT DO NOTHING`,
		MailboxCursor(in.Mailbox), appended.Seq, k.stamp(), in.Mailbox); err != nil {
		return Event{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, false, err
	}
	k.Hub.Publish(appended.Seq)
	return appended, true, nil
}

// PendingMessages are a mailbox's undelivered messages, oldest first.
func (k *Kernel) PendingMessages(ctx context.Context, address string, limit int) ([]Event, error) {
	cursor, err := k.GetCursor(ctx, MailboxCursor(address))
	if err != nil {
		return nil, err
	}
	return k.ListEvents(ctx, ListFilter{Mailbox: address, AfterSeq: &cursor, Limit: limit})
}

type PendingMailbox struct {
	Address string `json:"address"`
	Urgent  bool   `json:"urgent"`
	Count   int    `json:"count"`
}

// MailboxesWithPending lists mailboxes with undelivered messages: interrupt
// lane first, then oldest first.
func (k *Kernel) MailboxesWithPending(ctx context.Context) ([]PendingMailbox, error) {
	rows, err := k.DB.QueryContext(ctx, `
		SELECT substr(c.consumer, 9) AS address,
		       max(e.lane = 'interrupt') AS urgent,
		       count(*) AS n,
		       min(e.seq) AS oldest
		FROM cursors c
		JOIN events e ON e.mailbox = substr(c.consumer, 9) AND e.seq > c.seq
		WHERE c.consumer LIKE 'mailbox:%'
		GROUP BY c.consumer
		ORDER BY urgent DESC, oldest ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingMailbox
	for rows.Next() {
		var p PendingMailbox
		var urgent int
		var oldest int64
		if err := rows.Scan(&p.Address, &urgent, &p.Count, &oldest); err != nil {
			return nil, err
		}
		p.Urgent = urgent == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Addresses of the agent loops.
const (
	Primary       = "primary"
	ManagerPrefix = "manager:"
)

func ManagerAddress(workItemID string) string { return ManagerPrefix + workItemID }
func IsManagerAddress(address string) bool    { return strings.HasPrefix(address, ManagerPrefix) }
func WorkItemIDOf(address string) string      { return strings.TrimPrefix(address, ManagerPrefix) }
