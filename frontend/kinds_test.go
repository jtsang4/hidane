package frontend

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/jtsang4/hidane/internal/kernel"
)

// The SPA filters live events with its own copy of the kernel's kind lists: a
// kind added on one side only leaves the conversation stale or a reply doubled.
func TestKindListsMatchTheKernel(t *testing.T) {
	src, err := os.ReadFile("src/lib/kinds.ts")
	if err != nil {
		t.Fatal(err)
	}
	sets := map[string][]string{}
	for _, m := range regexp.MustCompile(`const (\w+)[^=]*= new Set\(\[([^\]]*)\]\)`).FindAllStringSubmatch(string(src), -1) {
		for _, item := range regexp.MustCompile(`"([^"]+)"|\.\.\.(\w+)`).FindAllStringSubmatch(m[2], -1) {
			if item[1] != "" {
				sets[m[1]] = append(sets[m[1]], item[1])
			} else {
				sets[m[1]] = append(sets[m[1]], sets[item[2]]...)
			}
		}
	}
	for name, want := range map[string][]string{
		"ANSWER_KINDS":     kernel.AnswerKinds,
		"MAIN_KINDS":       kernel.ConversationMainKinds,
		"ANY_THREAD_KINDS": kernel.ConversationAnyKinds,
	} {
		got, want := slices.Sorted(slices.Values(sets[name])), slices.Sorted(slices.Values(want))
		if !slices.Equal(got, want) {
			t.Errorf("kinds.ts %s = %v, the kernel's = %v", name, got, want)
		}
	}
}
