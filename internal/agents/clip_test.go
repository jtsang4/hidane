package agents

import (
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/clip"
)

// Only an answer far beyond any real one is cut; how the cut reads is clip's.
func TestOnlyAHugeAnswerIsCut(t *testing.T) {
	if maxAnswerRunes < 200_000 {
		t.Fatalf("answers are cut at %d runes", maxAnswerRunes)
	}
	long := strings.Repeat("长", 20_000)
	if clip.Noted(long, maxAnswerRunes) != long {
		t.Fatal("a long answer is stored whole")
	}
}
