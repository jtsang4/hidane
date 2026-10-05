package feishu_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/feishu"
	"github.com/jtsang4/hidane/internal/kernel"
)

var partLabel = regexp.MustCompile(`\n\n（(\d+)/(\d+)）$`)

// Every part a long answer is sent in stays within Feishu's limit with its
// "（i/n）" label counted (acceptance once saw 3507 runes against 3500), and
// the parts, labels removed, still add up to the whole answer.
func TestSentPartsStayWithinTheLimitLabelIncluded(t *testing.T) {
	var paragraphs []string
	for i := 0; i < 600; i++ {
		paragraphs = append(paragraphs, "段落内容。")
	}
	var wide []string
	for i := 0; i < 40; i++ {
		wide = append(wide, strings.Repeat(string(rune('a'+i%26)), 1000))
	}
	for _, c := range []struct {
		name, text, sep string
	}{
		// 500 paragraphs fill a part to 3498 runes before its label.
		{"paragraphs", strings.Join(paragraphs, "\n\n"), "\n\n"},
		{"ten parts or more", strings.Join(wide, "\n\n"), "\n\n"},
		{"one oversized line", strings.Repeat("x", 9000), ""},
		// Nine parts without the label's room, ten with it: the room grows
		// with the count's digits.
		{"the count gains a digit", strings.Repeat("y", 31500), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			k, ch, fm := setup(t)
			m(ch.OutboxOnce(ctx))
			_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_q", ChatID: "oc_1", ChatType: "p2p", MessageType: "text", Content: text("问题")})
			q := m(k.PendingMessages(ctx, kernel.Primary, 10))[0]
			m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: kernel.Payload{"text": c.text, "root": q.ID}}))
			m(ch.OutboxOnce(ctx))
			if len(fm.sent) < 2 {
				t.Fatalf("a long answer is sent in parts: %d", len(fm.sent))
			}
			var bodies []string
			for i, s := range fm.sent {
				if n := len([]rune(s.text)); n > 3500 {
					t.Fatalf("part %d is %d runes, over the 3500 limit", i+1, n)
				}
				label := partLabel.FindStringSubmatch(s.text)
				if label == nil || label[1] != fmt.Sprint(i+1) || label[2] != fmt.Sprint(len(fm.sent)) {
					t.Fatalf("part %d is not numbered %d/%d: %q", i+1, i+1, len(fm.sent), s.text[max(0, len(s.text)-30):])
				}
				bodies = append(bodies, strings.TrimSuffix(s.text, label[0]))
			}
			if strings.Join(bodies, c.sep) != c.text {
				t.Fatal("the parts do not add up to the answer")
			}
		})
	}
}
