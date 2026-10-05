package agents_test

import (
	"testing"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/connectors"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// "Only mine" (origin=person) is the person's conversation: the Primary's
// answer to a scheduled prompt stays in the full view and out of that one.
func TestOnlyMineLeavesOutAnswersToSchedules(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	name, action, every := "早报", "prompt", 3600
	sc := m(w.k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &every,
		Spec: &kernel.ScheduleSpec{Prompt: "你好，今天的早报"}}, "test"))
	m(connectors.FireSchedule(ctx, w.k, sc))
	said := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "你好，我自己问的", Source: "connector:web"}))
	w.settle()
	prompts := w.events("schedule.prompt")
	if len(prompts) != 1 {
		t.Fatalf("the schedule spoke to the Primary: %v", w.kinds())
	}
	answers := map[string]kernel.Event{}
	for _, r := range w.events("agent.reply") {
		answers[r.Payload.Str("root")] = r
	}
	scheduled, person := answers[prompts[0].ID], answers[said.ID]
	if scheduled.ID == "" || scheduled.Payload.Str("rootKind") != "scheduled" || person.ID == "" {
		t.Fatalf("both answered, the schedule's marked as such: %+v", answers)
	}
	listed := func(personOnly bool) map[string]bool {
		out := map[string]bool{}
		for _, e := range m(w.k.ListEvents(ctx, kernel.ListFilter{Conversation: true, PersonOnly: personOnly})) {
			out[e.ID] = true
		}
		return out
	}
	all, mine := listed(false), listed(true)
	if !all[scheduled.ID] || !all[person.ID] {
		t.Fatal("the full conversation shows both answers")
	}
	if mine[scheduled.ID] || !mine[person.ID] || !mine[said.ID] {
		t.Fatalf("only mine keeps the person's words and their answer, not the schedule's: %v", mine)
	}
}
