package api_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// A task made directly answers 201; with a brief its Manager wakes up and
// takes a turn on it, without one nobody does. Done and closed are two
// states: both leave the default list, each keeps its own name.
func TestADirectTaskWakesItsManagerOnlyWithABrief(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := t.Context()
	create := func(body map[string]any) map[string]any {
		t.Helper()
		code, res := e.do("POST", "/api/work-items", "", body)
		if code != 201 || res["dispatched"] != (body["brief"] != nil) {
			t.Fatalf("create %v: %d %v", body, code, res)
		}
		return res["item"].(map[string]any)
	}
	briefed := create(map[string]any{"title": "now", "brief": "write the notes"})["id"].(string)
	quiet := create(map[string]any{"title": "later"})["id"].(string)
	if err := e.s.NewRuntime().Drain(10); err != nil {
		t.Fatal(err)
	}
	turns := map[string]int{}
	for _, d := range m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "manager.decision"})) {
		turns[d.WorkItemID]++
	}
	if turns[briefed] == 0 || turns[quiet] != 0 {
		t.Fatalf("only the brief wakes a Manager: %v", turns)
	}

	for id, status := range map[string]string{briefed: kernel.StatusDone, quiet: kernel.StatusClosed} {
		if code, _ := e.do("PATCH", "/api/work-items/"+id, "", map[string]any{"status": status}); code != 200 {
			t.Fatalf("%s: %d", status, code)
		}
	}
	statuses := func(query string) map[string]any {
		_, body := e.do("GET", "/api/work-items"+query, "", nil)
		out := map[string]any{}
		for _, it := range body["items"].([]any) {
			out[it.(map[string]any)["id"].(string)] = it.(map[string]any)["status"]
		}
		return out
	}
	if listed := statuses(""); len(listed) != 0 {
		t.Fatalf("neither a done nor a closed task is in the default list: %v", listed)
	}
	if all := statuses("?all"); all[briefed] != kernel.StatusDone || all[quiet] != kernel.StatusClosed {
		t.Fatalf("?all lists each with its own status: %v", all)
	}
	_, board := e.do("GET", "/api/board", "", nil)
	states := map[string]any{}
	for _, c := range board["cards"].([]any) {
		card := c.(map[string]any)
		states[card["item"].(map[string]any)["id"].(string)] = card["state"]
	}
	if states[briefed] != "done" || states[quiet] != "closed" {
		t.Fatalf("the board tells done from closed: %v", states)
	}
}
