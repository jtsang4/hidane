package feishu_test

import (
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/feishu"
)

// A long answer is sent in parts split at paragraph breaks, each within
// Feishu's limit, and nothing is lost: a cut once read as the runtime hanging.
func TestALongAnswerIsSentWhole(t *testing.T) {
	var paragraphs []string
	for i := 0; i < 40; i++ {
		paragraphs = append(paragraphs, strings.Repeat(string(rune('a'+i%26)), 300))
	}
	text := strings.Join(paragraphs, "\n\n")
	chunks := feishu.ChunkText(text, 3500)
	for _, c := range chunks {
		if n := len([]rune(c)); n > 3500 || strings.HasPrefix(c, "\n") || strings.HasSuffix(c, "\n") {
			t.Fatalf("a part of %d runes, or one split inside a paragraph break", n)
		}
	}
	if len(chunks) < 4 || strings.Join(chunks, "\n\n") != text {
		t.Fatalf("%d parts that do not add up to the answer", len(chunks))
	}
}
