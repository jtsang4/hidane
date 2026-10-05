// Package projections are derived, rebuildable, read-only views over the log:
// the conversation as history, the task board, the daily worklog and archive.
// Nothing here is a second source of truth.
package projections

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/clip"
	"github.com/jtsang4/hidane/internal/kernel"
)

// VisibleSQL is what was said, for search and the day index — narrower than
// kernel.ConversationSQL, which also holds routing, errors and steer notes:
// the person's messages and questions on the main thread, plus answers from
// any thread that name the message they answer. A subtask's report to its
// parent is not shown.
const VisibleSQL = `(events.kind IN ('user.message', 'agent.reply', 'escalation')
  AND (events.thread_id = 'main' OR (events.kind = 'agent.reply' AND json_extract(events.payload, '$.root') IS NOT NULL))
  AND coalesce(json_extract(events.payload, '$.child'), 0) <> 1)`

// SaidSQL is the searchable words of a conversation event.
const SaidSQL = `(coalesce(json_extract(events.payload, '$.text'), '') || ' ' || coalesce(json_extract(events.payload, '$.question'), ''))`

const maxTerms = 8

// SearchTerms are whitespace-separated terms, all of which must match.
func SearchTerms(query string) []string {
	var out []string
	for _, t := range strings.Fields(query) {
		if len(t) > 100 {
			t = t[:100]
		}
		out = append(out, t)
		if len(out) == maxTerms {
			break
		}
	}
	return out
}

func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}

type SearchPage struct {
	Events  []kernel.Event `json:"events"`
	HasMore bool           `json:"hasMore"`
}

// SearchConversation is newest-first search over everything ever said. Hidden
// messages never match, whatever their original words.
func SearchConversation(ctx context.Context, k *kernel.Kernel, query string, before *int64, limit int) (SearchPage, error) {
	terms := SearchTerms(query)
	page := SearchPage{Events: []kernel.Event{}}
	if len(terms) == 0 {
		return page, nil
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{VisibleSQL, "NOT " + kernel.RedactedSQL}
	var args []any
	for _, t := range terms {
		where = append(where, SaidSQL+` LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(t))
	}
	if before != nil {
		where = append(where, "events.seq < ?")
		args = append(args, *before)
	}
	rows, err := k.DB.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM events WHERE %s ORDER BY events.seq DESC LIMIT %d`,
		kernel.SelectCols, strings.Join(where, " AND "), limit+1), args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := kernel.ScanEvent(rows)
		if err != nil {
			return page, err
		}
		page.Events = append(page.Events, e)
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		page.HasMore = true
	}
	return page, rows.Err()
}

// SearchWorkItems finds work items (any status) whose id or title holds every term.
func SearchWorkItems(ctx context.Context, k *kernel.Kernel, query string, limit int) ([]kernel.WorkItem, error) {
	terms := SearchTerms(query)
	out := []kernel.WorkItem{}
	if len(terms) == 0 {
		return out, nil
	}
	items, err := k.ListWorkItems(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := len(items) - 1; i >= 0 && len(out) < limit; i-- {
		hay := strings.ToLower(items[i].ID + " " + items[i].Title)
		all := true
		for _, t := range terms {
			if !strings.Contains(hay, strings.ToLower(t)) {
				all = false
				break
			}
		}
		if all {
			out = append(out, items[i])
		}
	}
	return out, nil
}

type ConversationDay struct {
	Day     string `json:"day"`
	Count   int    `json:"count"`
	FirstID string `json:"firstId"`
}

// ConversationDays lists every day something was said, newest first.
func ConversationDays(ctx context.Context, k *kernel.Kernel, zone string) ([]ConversationDay, error) {
	loc, err := time.LoadLocation(zone) // "" is UTC
	if err != nil {
		loc = time.UTC
	}
	rows, err := k.DB.QueryContext(ctx, `SELECT events.id, events.ts FROM events WHERE `+VisibleSQL+` ORDER BY events.seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]*ConversationDay{}
	var order []string
	for rows.Next() {
		var id, ts string
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		t, err := kernel.ParseTime(ts)
		if err != nil {
			continue
		}
		day := t.In(loc).Format("2006-01-02")
		d := byDay[day]
		if d == nil {
			d = &ConversationDay{Day: day, FirstID: id}
			byDay[day] = d
			order = append(order, day)
		}
		d.Count++
	}
	sort.Sort(sort.Reverse(sort.StringSlice(order)))
	out := []ConversationDay{}
	for _, day := range order {
		out = append(out, *byDay[day])
	}
	return out, rows.Err()
}

// TitlesFor names every work item the events mention: an item closed long ago
// is on no board, and the message that named it may be pages away.
func TitlesFor(ctx context.Context, k *kernel.Kernel, events []kernel.Event) (map[string]string, error) {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, e := range events {
		add(e.WorkItemID)
		add(e.Payload.Str("workItemId"))
	}
	return k.TitlesFor(ctx, ids)
}

func stamp(iso string) string {
	t, err := kernel.ParseTime(iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("01-02 15:04")
}

// How much of the conversation the Primary reads each turn.
const (
	ContextTurns = 12
	ContextChars = 6000
)

type RecentConversation struct {
	Text   string  `json:"-"`
	FromID *string `json:"fromId"`
	Turns  int     `json:"turns"`
}

type contextTurn struct {
	root    string
	seq     int64
	ts      string
	said    *string
	origin  string
	routed  string
	answers []string
	hidden  bool
	// multi: the person addressed several work items at once; every one counts.
	multi bool
}

// Recent is the Primary's view of what was said before this turn: the newest
// turns of the conversation, bounded by count and size, rebuilt from the log
// every time. It replaces an ever-growing model session: the same before and
// after a restart, never compacted mid-turn, and showable to the person,
// because the view calls this same function.
func Recent(ctx context.Context, k *kernel.Kernel, exclude map[string]bool) (RecentConversation, error) {
	events, err := k.ListEvents(ctx, kernel.ListFilter{Conversation: true, Tail: 400})
	if err != nil {
		return RecentConversation{}, err
	}
	turns := map[string]*contextTurn{}
	turnFor := func(root string, e kernel.Event) *contextTurn {
		t := turns[root]
		if t == nil {
			t = &contextTurn{root: root, seq: e.Seq, ts: e.TS}
			turns[root] = t
		}
		return t
	}
	for _, e := range events {
		of, root := e.Payload.Str("of"), e.Payload.Str("root")
		switch {
		case e.Kind == "user.message" && e.ThreadID == "main":
			t := turnFor(e.ID, e)
			t.seq, t.ts = e.Seq, e.TS
			if list, ok := e.Payload["targets"].([]any); ok && len(list) > 1 {
				t.multi = true
			}
			if e.Payload.Bool("redacted") {
				t.hidden = true
			} else {
				s := e.Payload.Str("text")
				t.said = &s
			}
		case e.Kind == "message.redacted" && of != "":
			turnFor(of, e).hidden = true
		case e.Kind == "message.attributed" && of != "":
			t := turnFor(of, e)
			to := fmt.Sprintf("%s %q", e.Payload.Str("workItemId"), e.Payload.Str("title"))
			if t.multi && t.routed != "" {
				t.routed += ", " + to
			} else {
				t.routed = to
			}
		// A "which task?" question is settled by the attribution that follows;
		// a steer note repeats the person's words.
		case root != "" && e.Kind != "attribution.ambiguous" && e.Kind != "execution.steered" && slices.Contains(kernel.AnswerKinds, e.Kind):
			if e.Payload.Bool("child") {
				continue
			}
			t := turnFor(root, e)
			label := e.Payload.Str("rootKind")
			if t.said == nil && (label == "external" || label == "scheduled" || label == "repo") {
				t.origin = label + ": " + e.Payload.Str("rootText")
			}
			words := e.Payload.Str("text")
			if e.Kind == "escalation" {
				words = e.Payload.Str("question")
			} else if words == "" {
				words = e.Payload.Str("error")
			}
			if strings.TrimSpace(words) != "" {
				who := "answer"
				if e.Kind == "escalation" {
					who = "question"
				}
				if e.WorkItemID != "" {
					who += " (" + e.WorkItemID + ")"
				}
				t.answers = append(t.answers, who+": "+clip.Line(words, 400))
			}
		}
	}
	var ordered []*contextTurn
	for _, t := range turns {
		if t.hidden || exclude[t.root] || (t.said == nil && t.origin == "") {
			continue
		}
		ordered = append(ordered, t)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].seq < ordered[j].seq })
	var picked []string
	used := 0
	var from string
	for i := len(ordered) - 1; i >= 0 && len(picked) < ContextTurns; i-- {
		t := ordered[i]
		head := fmt.Sprintf("[%s] %s ", t.root, stamp(t.ts))
		if t.said != nil {
			head += "person: " + clip.Line(*t.said, 300)
		} else {
			head += "(" + clip.Line(t.origin, 200) + ")"
		}
		lines := []string{head}
		if t.routed != "" {
			label := "  → work item "
			if t.multi {
				label = "  → work items "
			}
			lines = append(lines, label+t.routed)
		}
		answers := t.answers
		if len(answers) > 2 {
			answers = answers[len(answers)-2:]
		}
		for _, a := range answers {
			lines = append(lines, "  → "+a)
		}
		block := strings.Join(lines, "\n")
		if len(picked) > 0 && used+len(block) > ContextChars {
			break
		}
		picked = append([]string{block}, picked...)
		used += len(block)
		from = t.root
	}
	if len(picked) == 0 {
		return RecentConversation{}, nil
	}
	return RecentConversation{
		Text:   "Recent conversation (oldest first; already answered — context only, do not answer again):\n" + strings.Join(picked, "\n"),
		FromID: &from,
		Turns:  len(picked),
	}, nil
}

// DescribeRecall renders search results for a Primary that asked to look further back.
func DescribeRecall(events []kernel.Event) string {
	if len(events) == 0 {
		return "(nothing in the conversation history matched)"
	}
	sorted := append([]kernel.Event{}, events...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	var lines []string
	for _, e := range sorted {
		who, words := "answer", e.Payload.Str("text")
		switch e.Kind {
		case "user.message":
			who = "person"
		case "escalation":
			who, words = "question", e.Payload.Str("question")
		}
		if e.WorkItemID != "" {
			who += " (" + e.WorkItemID + ")"
		}
		// Local time, like the "Now:" line the model reads it against: a UTC
		// date put an evening message on the previous day.
		when := e.TS
		if t, err := kernel.ParseTime(e.TS); err == nil {
			when = t.Local().Format("2006-01-02 15:04")
		}
		lines = append(lines, fmt.Sprintf("- %s %s: %s", when, who, clip.Line(words, 400)))
	}
	return strings.Join(lines, "\n")
}
