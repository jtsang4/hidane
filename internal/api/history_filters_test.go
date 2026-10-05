package api_test

import (
	"strconv"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// A task's detail is everything recorded for it, whichever thread it was
// written on: what was said to it on the main thread, its own thread, and the
// thread copies older runtimes wrote.
func TestTheTaskDetailListsItsEventsAcrossThreads(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := t.Context()
	item := m(e.k.CreateWorkItem(ctx, "notes", "test", kernel.CreateWorkItemOpts{}))
	add := func(thread, kind, workItem string) kernel.Event {
		return m(e.k.Append(ctx, kernel.EventInput{Source: "test", Kind: kind, ThreadID: thread, WorkItemID: workItem}))
	}
	want := map[string]string{}
	for _, ev := range []kernel.Event{
		add("main", "user.message", item.ID),
		add("main", "message.attributed", item.ID),
		add(item.ThreadID, "user.message", item.ID),
		add(item.ThreadID, "agent.reply", item.ID),
		add("main", "escalation", item.ID),
		add("th_older", "agent.reply", item.ID),
	} {
		want[ev.ID] = ev.ThreadID
	}
	other := add("main", "user.message", "")
	_, detail := e.do("GET", "/api/work-items/"+item.ID+"?limit=50", "", nil)
	got := map[string]bool{}
	for _, ev := range eventsOf(detail) {
		got[ev["id"].(string)] = true
	}
	for id, thread := range want {
		if !got[id] {
			t.Errorf("the detail leaves out %s, written on %s", id, thread)
		}
	}
	if got[other.ID] {
		t.Error("the detail lists an event of no task")
	}
}

// The event log page filters by several kinds at once, in the query, and keeps
// the filter when it pages back; one kind still works alone.
func TestTheEventsPageFiltersSeveralKinds(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := t.Context()
	item := m(e.k.CreateWorkItem(ctx, "noisy", "test", kernel.CreateWorkItemOpts{}))
	for i := 0; i < 3; i++ {
		for _, kind := range []string{"agent.reply", "side_effect.intent", "escalation", "side_effect.result"} {
			m(e.k.Append(ctx, kernel.EventInput{Source: "test", Kind: kind, ThreadID: item.ThreadID, WorkItemID: item.ID}))
		}
	}
	m(e.k.Append(ctx, kernel.EventInput{Source: "test", Kind: "agent.reply", ThreadID: "main"}))
	page := func(query string) ([]map[string]any, bool) {
		_, body := e.do("GET", "/api/events?page=1&item="+item.ID+"&"+query, "", nil)
		return eventsOf(body), body["hasMore"] == true
	}
	var seen []string
	events, more := page("kind=agent.reply,%20escalation&limit=4")
	for i := 0; ; i++ {
		for _, ev := range events {
			seen = append(seen, ev["kind"].(string))
		}
		if !more || len(events) == 0 || i == 5 {
			break
		}
		events, more = page("kind=agent.reply,escalation&limit=4&before=" + strconv.FormatInt(int64(events[0]["seq"].(float64)), 10))
	}
	counts := map[string]int{}
	for _, kind := range seen {
		counts[kind]++
	}
	if len(seen) != 6 || counts["agent.reply"] != 3 || counts["escalation"] != 3 {
		t.Fatalf("the task's replies and questions, nothing else, across pages: %v", seen)
	}
	if one, _ := page("kind=escalation&limit=50"); len(one) != 3 || one[0]["kind"] != "escalation" {
		t.Fatalf("one kind: %v", one)
	}
}
