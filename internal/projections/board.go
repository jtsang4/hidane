package projections

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/kernel"
)

type BoardCard struct {
	Item            kernel.WorkItem `json:"item"`
	State           string          `json:"state"`
	Understanding   *string         `json:"understanding"`
	LastReply       *CardReply      `json:"lastReply"`
	Execution       *CardExecution  `json:"execution"`
	Escalation      *CardEscalation `json:"escalation"`
	LastPolicyBlock *CardBlock      `json:"lastPolicyBlock"`
	Anchor          *string         `json:"anchor"`
	ChildIDs        []string        `json:"childIds"`
	LastSeq         int64           `json:"lastSeq"`
	Checkouts       []CardCheckout  `json:"checkouts"`
}

// CardCheckout is a repository the work item works in, as recorded — the
// card is polled often, so it never asks git.
type CardCheckout struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Mode    string `json:"mode"`
	Status  string `json:"status"`
	Setup   string `json:"setup"`
	Missing bool   `json:"missing"`
	// Continues is the branch this one was started from when that is not the
	// repo's default — earlier work it carries on.
	Continues string `json:"continues"`
}

type CardReply struct {
	Text string `json:"text"`
	TS   string `json:"ts"`
	Seq  int64  `json:"seq"`
}

type CardExecution struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	StartedAt    *string `json:"startedAt"`
	Instructions string  `json:"instructions"`
	ToolCalls    int     `json:"toolCalls"`
	LastTool     *string `json:"lastTool"`
}

type CardEscalation struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Reason   string `json:"reason"`
	Path     []any  `json:"path"`
	// Options are answers offered to pick from; empty for an open question.
	Options []string `json:"options"`
	TS      string   `json:"ts"`
}

type CardBlock struct {
	Reason string `json:"reason"`
	TS     string `json:"ts"`
}

type latestRow struct {
	seq         int64
	id, ts      string
	kind        string
	workItemID  string
	executionID string
	payload     kernel.Payload
}

const recent = 24 * time.Hour

var spaces = regexp.MustCompile(`\s+`)

// DescribeTool puts a tool call in words a person scans: the command, or the file it touched.
func DescribeTool(p kernel.Payload) string {
	tool := p.Str("tool")
	raw := p.Str("input")
	detail := raw
	var input map[string]any
	if json.Unmarshal([]byte(raw), &input) == nil {
		for _, key := range []string{"command", "path", "file_path", "pattern", "url"} {
			if s, ok := input[key].(string); ok {
				detail = s
				break
			}
		}
	}
	out := strings.TrimSpace(tool + " " + strings.TrimSpace(spaces.ReplaceAllString(detail, " ")))
	if r := []rune(out); len(r) > 160 {
		out = string(r[:160])
	}
	return out
}

// BuildBoard is the task-card view: open items plus anything closed within the last day.
func BuildBoard(ctx context.Context, k *kernel.Kernel, activeTurns []string) ([]BoardCard, error) {
	all, err := k.ListWorkItems(ctx, "")
	if err != nil {
		return nil, err
	}
	now := k.Now()
	var items []kernel.WorkItem
	for _, it := range all {
		updated, _ := kernel.ParseTime(it.UpdatedAt)
		if it.Status == kernel.StatusOpen || now.Sub(updated) < recent {
			items = append(items, it)
		}
	}
	cards := []BoardCard{}
	if len(items) == 0 {
		return cards, nil
	}
	args := make([]any, len(items))
	for i, it := range items {
		args[i] = it.ID
	}
	in := kernel.Placeholders(len(items))
	rows, err := k.DB.QueryContext(ctx, `
		SELECT e.seq, e.id, e.ts, e.kind, e.work_item_id, e.execution_id, e.payload FROM events e
		JOIN (SELECT work_item_id, kind, max(seq) AS seq FROM events
		      WHERE work_item_id IN (`+in+`)
		        AND (kind IN ('work_item.understanding', 'agent.reply', 'user.message', 'work_item.created', 'policy.blocked', 'side_effect.intent')
		             OR (kind = 'escalation' AND json_extract(payload, '$.question') IS NOT NULL))
		      GROUP BY work_item_id, kind) latest ON latest.seq = e.seq`, args...)
	if err != nil {
		return nil, err
	}
	byItem := map[string]map[string]latestRow{}
	for rows.Next() {
		var r latestRow
		var exec sql.NullString
		var payload string
		if err := rows.Scan(&r.seq, &r.id, &r.ts, &r.kind, &r.workItemID, &exec, &payload); err != nil {
			rows.Close()
			return nil, err
		}
		r.executionID = exec.String
		_ = json.Unmarshal([]byte(payload), &r.payload)
		if byItem[r.workItemID] == nil {
			byItem[r.workItemID] = map[string]latestRow{}
		}
		byItem[r.workItemID][r.kind] = r
	}
	rows.Close()
	maxSeq := map[string]int64{}
	mrows, err := k.DB.QueryContext(ctx, `SELECT work_item_id, max(seq) FROM events WHERE work_item_id IN (`+in+`) GROUP BY work_item_id`, args...)
	if err != nil {
		return nil, err
	}
	for mrows.Next() {
		var id string
		var seq int64
		if err := mrows.Scan(&id, &seq); err != nil {
			mrows.Close()
			return nil, err
		}
		maxSeq[id] = seq
	}
	mrows.Close()
	executions, err := k.ActiveExecutions(ctx)
	if err != nil {
		return nil, err
	}
	pendingList, err := k.MailboxesWithPending(ctx)
	if err != nil {
		return nil, err
	}
	pending := map[string]bool{}
	for _, p := range pendingList {
		pending[p.Address] = true
	}
	turns := map[string]bool{}
	for _, a := range activeTurns {
		turns[a] = true
	}
	checkouts, err := k.ListCheckouts(ctx, kernel.CheckoutFilter{})
	if err != nil {
		return nil, err
	}
	repoList, err := k.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	repoByID := map[string]kernel.Repo{}
	for _, r := range repoList {
		repoByID[r.ID] = r
	}
	held := map[string][]CardCheckout{}
	for _, c := range checkouts {
		r, ok := repoByID[c.RepoID]
		if !ok {
			r.Name = c.RepoID
		}
		card := CardCheckout{ID: c.ID, Repo: r.Name, Branch: c.Branch, Mode: c.Mode,
			Status: c.Status, Setup: c.Setup, Missing: !ok || r.Status == kernel.RepoMissing}
		if c.Base != "" && c.Base != c.Branch && c.Base != r.DefaultBranch {
			card.Continues = c.Base
		}
		held[c.WorkItemID] = append(held[c.WorkItemID], card)
	}
	for _, item := range items {
		latest := byItem[item.ID]
		esc, hasEsc := latest["escalation"]
		lastUser, hasUser := latest["user.message"]
		var open *latestRow
		if hasEsc && esc.payload.Str("question") != "" && (!hasUser || lastUser.seq < esc.seq) && item.Status == kernel.StatusOpen {
			open = &esc
		}
		var execution *kernel.Execution
		for i := range executions {
			if executions[i].WorkItemID == item.ID {
				execution = &executions[i]
				break
			}
		}
		var execView *CardExecution
		if execution != nil {
			var startedPayload string
			_ = k.DB.QueryRowContext(ctx, `SELECT payload FROM events WHERE execution_id = ? AND kind = 'execution.started' LIMIT 1`, execution.ID).Scan(&startedPayload)
			var sp kernel.Payload
			_ = json.Unmarshal([]byte(startedPayload), &sp)
			var count int
			_ = k.DB.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE execution_id = ? AND kind = 'side_effect.intent'`, execution.ID).Scan(&count)
			execView = &CardExecution{ID: execution.ID, Status: execution.Status, StartedAt: execution.StartedAt,
				Instructions: sp.Str("instructions"), ToolCalls: count}
			if lt, ok := latest["side_effect.intent"]; ok && lt.executionID == execution.ID {
				d := DescribeTool(lt.payload)
				execView.LastTool = &d
			}
		}
		address := kernel.ManagerAddress(item.ID)
		hasOpenChild := false
		var childIDs []string
		for _, c := range items {
			if c.Parent() == item.ID {
				childIDs = append(childIDs, c.ID)
				if c.Status == kernel.StatusOpen {
					hasOpenChild = true
				}
			}
		}
		state := item.Status
		switch {
		case open != nil:
			state = "waiting"
		case execution != nil && execution.Status == kernel.ExecRunning:
			state = "running"
		case execution != nil:
			state = "queued"
		case pending[address] || turns[address]:
			state = "thinking"
		case item.Status == kernel.StatusOpen && hasOpenChild:
			state = "delegated"
		case item.Status == kernel.StatusOpen && reportPending(item, latest):
			state = "review"
		case item.Status == kernel.StatusOpen:
			state = "idle"
		}
		card := BoardCard{Item: item, State: state, Execution: execView, ChildIDs: childIDs, LastSeq: maxSeq[item.ID], Checkouts: held[item.ID]}
		if card.ChildIDs == nil {
			card.ChildIDs = []string{}
		}
		if card.Checkouts == nil {
			card.Checkouts = []CardCheckout{}
		}
		if u, ok := latest["work_item.understanding"]; ok {
			s := u.payload.Str("text")
			card.Understanding = &s
		}
		if r, ok := latest["agent.reply"]; ok {
			text := r.payload.Str("text")
			if rr := []rune(text); len(rr) > 1200 {
				text = string(rr[:1200])
			}
			card.LastReply = &CardReply{Text: text, TS: r.ts, Seq: r.seq}
		}
		if open != nil {
			reason := open.payload.Str("reason")
			if reason == "" {
				reason = "question"
			}
			path, _ := open.payload["path"].([]any)
			if path == nil {
				path = []any{}
			}
			card.Escalation = &CardEscalation{ID: open.id, Question: open.payload.Str("question"), Reason: reason, Path: path, Options: []string{}, TS: open.ts}
			if list, ok := open.payload["options"].([]any); ok {
				for _, o := range list {
					if s, ok := o.(string); ok && s != "" {
						card.Escalation.Options = append(card.Escalation.Options, s)
					}
				}
			}
		}
		if b, ok := latest["policy.blocked"]; ok {
			card.LastPolicyBlock = &CardBlock{Reason: b.payload.Str("reason"), TS: b.ts}
		}
		if c, ok := latest["work_item.created"]; ok && c.payload.Str("of") != "" {
			of := c.payload.Str("of")
			card.Anchor = &of
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// reportPending: the work reported back and the person has not answered it,
// marked it done or closed it — the result waits for their review. Only for a
// top-level item: a subtask reports to its parent, not to the person.
func reportPending(item kernel.WorkItem, latest map[string]latestRow) bool {
	if item.Parent() != "" {
		return false
	}
	reply, ok := latest["agent.reply"]
	if !ok || !reply.payload.Bool("report") {
		return false
	}
	said, ok := latest["user.message"]
	return !ok || said.seq < reply.seq
}

// RenderDay is the daily worklog: a projection of the log, never a second source of truth.
func RenderDay(ctx context.Context, k *kernel.Kernel, day string) (string, int, error) {
	events, err := k.ListEvents(ctx, kernel.ListFilter{Day: day})
	if err != nil {
		return "", 0, err
	}
	items, err := k.ListWorkItems(ctx, "")
	if err != nil {
		return "", 0, err
	}
	byThread := map[string][]kernel.Event{}
	var threads []string
	for _, e := range events {
		key := e.ThreadID
		if key == "" {
			key = "(no-thread)"
		}
		if _, ok := byThread[key]; !ok {
			threads = append(threads, key)
		}
		byThread[key] = append(byThread[key], e)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Worklog %s\n\nTotal events: %d\n\n", day, len(events))
	section := func(title string, evs []kernel.Event) {
		b.WriteString("## " + title + "\n\n")
		for _, e := range evs {
			b.WriteString(worklogLine(e) + "\n")
		}
		b.WriteString("\n")
	}
	if main, ok := byThread["main"]; ok {
		section("Main thread", main)
		delete(byThread, "main")
	}
	for _, it := range items {
		if evs, ok := byThread[it.ThreadID]; ok {
			section(fmt.Sprintf("%s — %s (%s)", it.ID, it.Title, it.Status), evs)
			delete(byThread, it.ThreadID)
		}
	}
	for _, th := range threads {
		if evs, ok := byThread[th]; ok {
			section("thread "+th, evs)
		}
	}
	return b.String(), len(events), nil
}

func worklogLine(e kernel.Event) string {
	t, _ := kernel.ParseTime(e.TS)
	at := t.Local().Format("15:04:05")
	if e.Payload.Bool("redacted") {
		return fmt.Sprintf("- `%s` **%s** (%s) (hidden by the person)", at, e.Kind, e.Source)
	}
	text := e.Payload.Str("text")
	if text == "" {
		text = e.Payload.Str("note")
	}
	if text == "" {
		text = e.Payload.Str("summary")
	}
	detail := text
	if detail == "" {
		b, _ := json.Marshal(e.Payload)
		detail = string(b)
		if r := []rune(detail); len(r) > 200 {
			detail = string(r[:200])
		}
	} else if r := []rune(detail); len(r) > 500 {
		detail = string(r[:500]) + "…"
	}
	return fmt.Sprintf("- `%s` **%s** (%s) %s", at, e.Kind, e.Source, strings.ReplaceAll(detail, "\n", " "))
}

// DayDir is worklogs/YYYY/MM/DD.
func DayDir(k *kernel.Kernel, day string) string {
	return filepath.Join(k.Cfg.WorklogsDir(), day[0:4], day[5:7], day[8:10])
}

// WriteDay writes the worklog projection to disk.
func WriteDay(ctx context.Context, k *kernel.Kernel, day string) (string, error) {
	md, _, err := RenderDay(ctx, k, day)
	if err != nil {
		return "", err
	}
	dir := DayDir(k, day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "worklog.md")
	return path, os.WriteFile(path, []byte(md), 0o644)
}

// ArchiveDay copies everything an agent or person needs to revisit a day into
// one directory: the worklog plus every agent session trace touched that day.
// Idempotent.
func ArchiveDay(ctx context.Context, k *kernel.Kernel, day string) (string, int, error) {
	if _, err := WriteDay(ctx, k, day); err != nil {
		return "", 0, err
	}
	dir := DayDir(k, day)
	out := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return dir, 0, err
	}
	start, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return dir, 0, err
	}
	end := start.AddDate(0, 0, 1)
	sources := map[string]string{"roles": k.Cfg.SessionsDir()}
	entries, _ := os.ReadDir(k.Cfg.WorkspacesDir())
	for _, ws := range entries {
		sources[ws.Name()] = filepath.Join(k.Cfg.WorkspacesDir(), ws.Name(), ".hidane", "sessions")
	}
	copied := 0
	for origin, src := range sources {
		_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(start) || !info.ModTime().Before(end) {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(src, path)
			name := origin + "-" + strings.ReplaceAll(rel, string(filepath.Separator), "-")
			if os.WriteFile(filepath.Join(out, name), b, 0o644) == nil {
				copied++
			}
			return nil
		})
	}
	return dir, copied, nil
}
