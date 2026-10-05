package kernel_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
)

// A schedule's last status is often an HTTP body; a byte cut at 200 split a
// Chinese character and stored invalid UTF-8.
func TestScheduleStatusIsClippedByCharacter(t *testing.T) {
	k := kerneltest.New(t)
	name, action, interval := "hourly", "prompt", 3600
	sc := m(k.CreateSchedule(ctx, kernel.ScheduleInput{Name: &name, Action: &action, IntervalSec: &interval, Spec: &kernel.ScheduleSpec{Prompt: "x"}}, "test"))
	status := "x" + strings.Repeat("中", 300) // byte 200 falls inside a character
	if err := k.MarkRun(ctx, sc.ID, status, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := *m(k.GetSchedule(ctx, sc.ID)).LastStatus
	if !utf8.ValidString(got) {
		t.Fatalf("stored status is not valid UTF-8: %q", got)
	}
	if want := "x" + strings.Repeat("中", 199); got != want {
		t.Fatalf("status = %q (%d runes), want the first 200 characters", got, utf8.RuneCountInString(got))
	}
}
