package api_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

func eventsOf(body map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range body["events"].([]any) {
		out = append(out, e.(map[string]any))
	}
	return out
}

// History loads a page at a time with the kinds filtered in the query, so a
// page of N rows renders N bubbles; whether a task runs comes from the
// executions table, not from the window of events that happens to be loaded.
func TestHistoryLoadsByThePage(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	add := func(in kernel.EventInput) kernel.Event { return m(e.k.Append(ctx, in)) }
	for i := 0; i < 4; i++ {
		add(kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main"})
		for j := 0; j < 3; j++ {
			add(kernel.EventInput{Source: "agent:primary", Kind: "route.decision", ThreadID: "main"})
		}
		add(kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main"})
	}
	for limit, more := range map[int]bool{5: true, 8: false} {
		_, body := e.do("GET", "/api/events?page=1&thread=main&kind=user.message,agent.reply&limit="+strconv.Itoa(limit), "", nil)
		page := eventsOf(body)
		if len(page) != limit || body["hasMore"] != more {
			t.Fatalf("limit %d: %d events, hasMore %v", limit, len(page), body["hasMore"])
		}
		for _, ev := range page {
			if ev["kind"] == "route.decision" {
				t.Fatalf("filtered in the query: %v", ev)
			}
		}
	}

	item := m(e.k.CreateWorkItem(ctx, "long run", "test", kernel.CreateWorkItemOpts{}))
	if err := e.k.CreateExecution(ctx, "ex_long", item.ID, kernel.ManagerAddress(item.ID)); err != nil {
		t.Fatal(err)
	}
	if err := e.k.SetExecutionStatus(ctx, "ex_long", kernel.ExecRunning); err != nil {
		t.Fatal(err)
	}
	started := add(kernel.EventInput{Source: "agent:manager", Kind: "execution.started", ThreadID: item.ThreadID, WorkItemID: item.ID, ExecutionID: "ex_long"})
	add(kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: kernel.Payload{"text": "forwarded copy"}})
	for i := 0; i < 20; i++ {
		add(kernel.EventInput{Source: "agent:worker", Kind: "side_effect.intent", ThreadID: item.ThreadID, WorkItemID: item.ID, ExecutionID: "ex_long"})
	}
	answer := add(kernel.EventInput{Source: "agent:manager", Kind: "agent.reply", ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: kernel.Payload{"root": started.ID}})

	// The conversation: what was said on the main thread, plus answers from any thread.
	_, body := e.do("GET", "/api/events?page=1&conversation=1&limit=200", "", nil)
	kinds := map[string]bool{}
	for _, ev := range eventsOf(body) {
		kinds[ev["kind"].(string)] = true
		if ev["kind"] == "user.message" && ev["threadId"] != "main" {
			t.Fatalf("a forwarded copy is not part of the conversation: %v", ev)
		}
	}
	if kinds["route.decision"] || kinds["side_effect.intent"] || !kinds["user.message"] || !kinds["agent.reply"] {
		t.Fatalf("conversation kinds: %v", kinds)
	}

	_, detail := e.do("GET", "/api/work-items/"+item.ID+"?limit=10", "", nil)
	page := eventsOf(detail)
	if len(page) != 10 || detail["hasMore"] != true || page[9]["id"] != answer.ID {
		t.Fatalf("the newest 10 of the item's events: %d %v", len(page), detail["hasMore"])
	}
	for _, ev := range page {
		if ev["id"] == started.ID {
			t.Fatal("the start must be outside the window for this test to mean anything")
		}
	}
	if detail["running"] != true {
		t.Fatal("a run whose start left the window still runs")
	}
	oldest := strconv.FormatInt(int64(page[0]["seq"].(float64)), 10)
	_, older := e.do("GET", "/api/events?item="+item.ID+"&before="+oldest+"&limit=10", "", nil)
	if prev := eventsOf(older); len(prev) != 10 || older["hasMore"] != true || prev[9]["seq"].(float64) >= page[0]["seq"].(float64) {
		t.Fatalf("paging back: %d %v", len(prev), older["hasMore"])
	}
	_, list := e.do("GET", "/api/work-items", "", nil)
	if running := list["running"].([]any); len(running) != 1 || running[0] != item.ID {
		t.Fatalf("the list's running: %v", running)
	}
	if err := e.k.SetExecutionStatus(ctx, "ex_long", kernel.ExecDone); err != nil {
		t.Fatal(err)
	}
	if _, detail := e.do("GET", "/api/work-items/"+item.ID+"?limit=10", "", nil); detail["running"] != false {
		t.Fatal("finished")
	}
	if _, list := e.do("GET", "/api/work-items", "", nil); len(list["running"].([]any)) != 0 {
		t.Fatalf("finished: %v", list["running"])
	}
}

// History is reachable by address, not only by scrolling: search covers what
// the conversation shows and the tasks by title, closed ones too; a window
// pages forward to the live edge; days are counted in the reader's zone.
func TestHistoryIsAddressable(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	evening := time.Date(2026, 10, 5, 23, 30, 0, 0, time.UTC) // already 10-06 in Shanghai
	e.k.Now = func() time.Time { return evening }
	add := func(in kernel.EventInput) kernel.Event { return m(e.k.Append(ctx, in)) }
	closed := m(e.k.CreateWorkItem(ctx, "rotate archive logs", "test", kernel.CreateWorkItemOpts{}))
	m(e.k.SetWorkItemStatus(ctx, closed.ID, kernel.StatusClosed, "test"))
	q := add(kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main", Payload: kernel.Payload{"text": "archive the logs"}})
	reply := func(thread string, p kernel.Payload) kernel.Event {
		return add(kernel.EventInput{Source: "agent:manager", Kind: "agent.reply", ThreadID: thread, WorkItemID: closed.ID, Payload: p})
	}
	done := reply(closed.ThreadID, kernel.Payload{"text": "archive done", "root": q.ID})
	reply(closed.ThreadID, kernel.Payload{"text": "archive child report", "root": q.ID, "child": true})
	reply("th_old", kernel.Payload{"text": "archive old copy"})
	add(kernel.EventInput{Source: "agent:primary", Kind: "user.message", ThreadID: closed.ThreadID, WorkItemID: closed.ID, Payload: kernel.Payload{"text": "archive the logs", "of": q.ID}})
	later := evening.Add(26 * time.Hour)
	e.k.Now = func() time.Time { return later }
	hook := add(kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: kernel.Payload{"text": "archive webhook answer", "root": "ev_hook", "rootKind": "external"}})

	_, found := e.do("GET", "/api/conversation/search?q=archive", "", nil)
	hits := map[any]bool{}
	for _, ev := range eventsOf(found) {
		hits[ev["id"]] = true
	}
	if len(hits) != 3 || !hits[q.ID] || !hits[done.ID] || !hits[hook.ID] {
		t.Fatalf("what the conversation shows, no subtask report or thread copy: %v", eventsOf(found))
	}
	if items := found["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != closed.ID {
		t.Fatalf("a closed task is found by its title: %v", items)
	}

	_, all := e.do("GET", "/api/events?page=1&conversation=1&limit=200", "", nil)
	if titles := all["titles"].(map[string]any); titles[closed.ID] != "rotate archive logs" {
		t.Fatalf("a closed task keeps its title in the conversation: %v", titles)
	}
	var paged []any
	after := "0"
	for i := 0; ; i++ {
		_, page := e.do("GET", "/api/events?page=1&conversation=1&limit=2&after="+after, "", nil)
		for _, ev := range eventsOf(page) {
			paged = append(paged, ev["id"])
		}
		if page["hasNewer"] == false {
			break
		}
		if i == 10 {
			t.Fatal("paging forward never reaches the live edge")
		}
		after = strconv.FormatInt(int64(page["newestSeq"].(float64)), 10)
	}
	if len(paged) != len(eventsOf(all)) {
		t.Fatalf("paging forward skipped or repeated: %d of %d", len(paged), len(eventsOf(all)))
	}
	_, person := e.do("GET", "/api/events?page=1&conversation=1&origin=person&limit=200", "", nil)
	for _, ev := range eventsOf(person) {
		if ev["id"] == hook.ID {
			t.Fatal("origin=person drops answers to webhooks")
		}
	}
	if len(eventsOf(person)) != len(eventsOf(all))-1 {
		t.Fatalf("origin=person keeps the rest: %d of %d", len(eventsOf(person)), len(eventsOf(all)))
	}

	for tz, first := range map[string]string{"Asia/Shanghai": "2026-10-06", "Not/AZone": "2026-10-05", "x'%20OR%201=1--": "2026-10-05"} {
		code, body := e.do("GET", "/api/conversation/days?tz="+tz, "", nil)
		days, _ := body["days"].([]any)
		if code != 200 || len(days) != 2 {
			t.Fatalf("%s: %d %v", tz, code, body)
		}
		newest, oldest := days[0].(map[string]any), days[1].(map[string]any)
		if newest["day"] != "2026-10-07" || newest["firstId"] != hook.ID || oldest["day"] != first || oldest["count"] != float64(2) || oldest["firstId"] != q.ID {
			t.Fatalf("%s: %v", tz, days)
		}
	}
}
