package agents_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// A worker's long account of its work reaches its outcome whole; only one far
// beyond any real answer is cut, and the cut says how much is missing.
func TestAWorkersLongSummaryIsKeptWhole(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	summaryOf := func(runes int) string {
		t.Helper()
		item := m(w.k.CreateWorkItem(ctx, "report", "test", kernel.CreateWorkItemOpts{}))
		m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "写报告 LONG_SUMMARY=" + strconv.Itoa(runes), Source: "connector:web", Target: item.ID}))
		w.settle()
		finished := m(w.k.ListEvents(ctx, kernel.ListFilter{Kind: "execution.finished", WorkItemID: item.ID}))
		if len(finished) != 1 || !finished[0].Payload.Bool("ok") {
			t.Fatalf("one run: %+v", finished)
		}
		return finished[0].Payload.Str("summary")
	}
	long := summaryOf(6000)
	if !strings.HasSuffix(long, strings.Repeat("长", 6000)) || strings.Contains(long, "more characters not shown") {
		t.Fatalf("a summary of several thousand characters is stored whole: %d runes", utf8.RuneCountInString(long))
	}
	huge := summaryOf(200_100)
	kept, note, cut := strings.Cut(huge, "\n\n[… ")
	if !cut || utf8.RuneCountInString(kept) != 200_000 || !regexp.MustCompile(`^[1-9]\d{2,} more characters not shown]$`).MatchString(note) {
		t.Fatalf("past 200,000 characters the summary is cut, saying how much: %d runes, note %q", utf8.RuneCountInString(kept), note)
	}
}

// A Primary that answers in prose twice — not the effect list, asked once
// more — has its text sent as the reply rather than swallowed.
func TestThePrimarysProseIsTheReplyAfterTheRetry(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.Claude)
	msg := m(w.s.SubmitMessage(ctx, agents.InboundMessage{Text: "PLAIN_PRIMARY 你在吗", Source: "connector:web"}))
	w.settle()
	decisions := w.events("route.decision")
	if len(decisions) != 2 || decisions[0].Payload.Bool("nudged") || !decisions[1].Payload.Bool("nudged") {
		t.Fatalf("asked once more, both answers on record: %+v", decisions)
	}
	replies := w.events("agent.reply")
	if len(replies) != 1 || replies[0].Payload.Str("root") != msg.ID || replies[0].Payload.Str("text") != "我直接回答，没有效果列表。" {
		t.Fatalf("the text is the reply: %+v", replies)
	}
	if items := m(w.k.ListWorkItems(ctx, "")); len(items) != 0 {
		t.Fatalf("nothing else happens: %+v", items)
	}
}
