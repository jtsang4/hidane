package agents

import (
	"strings"
	"testing"
	"time"
)

func TestReplyExtractorStreamsOnlyTheReply(t *testing.T) {
	raw := `{"effects":[{"type":"reply","of":"ev_1","reply":"你好\n世界 \"quoted\" é 😀 done"}]}`
	for _, size := range []int{1, 2, 3, 5, 13, len(raw)} {
		ex := ReplyExtractor()
		var out strings.Builder
		b := []byte(raw)
		for i := 0; i < len(b); {
			j := i + size
			if j > len(b) {
				j = len(b)
			}
			// Chunk on rune boundaries: CLIs hand out whole decoded strings.
			for j < len(b) && (b[j]&0xC0) == 0x80 {
				j++
			}
			out.WriteString(ex(string(b[i:j])))
			i = j
		}
		want := "你好\n世界 \"quoted\" é 😀 done"
		if out.String() != want {
			t.Fatalf("size %d: %q", size, out.String())
		}
	}
	if got := ReplyExtractor()(`{"type":"reply"}`); got != "" {
		t.Fatalf("a reply value is not the reply key: %q", got)
	}
}

func TestParseEffects(t *testing.T) {
	got := ParseEffects("Sure!\n```json\n{\"effects\":[{\"type\":\"reply\",\"reply\":\"x\"},{\"nope\":1}]}\n```")
	if len(got) != 1 || got[0]["reply"] != "x" {
		t.Fatalf("%+v", got)
	}
	if single := ParseEffects(`{"type":"done"}`); len(single) != 1 {
		t.Fatal("a single effect object counts")
	}
	if ParseEffects("plain words") != nil {
		t.Fatal("prose is not effects")
	}
	var v map[string]any
	if !ExtractJSON(`noise {"a":"}"} trailing`, &v) || v["a"] != "}" {
		t.Fatalf("braces inside strings: %+v", v)
	}
}

func TestBlockedQuestion(t *testing.T) {
	if q := BlockedQuestion("did stuff\nBLOCKED: which server?"); q != "which server?" {
		t.Fatal(q)
	}
	if BlockedQuestion("all good") != "" {
		t.Fatal("no question")
	}
}

func TestLiveTextSnapshotThenFrames(t *testing.T) {
	l := NewLiveText()
	h := l.Begin("main")
	h.Push("hel")
	snap, ch, cancel := l.Subscribe()
	defer cancel()
	if len(snap) != 1 || snap[0].Text != "hel" {
		t.Fatalf("a client joining mid-run gets the text so far: %+v", snap)
	}
	h.Push("lo")
	h.End()
	h.End()
	var frames []LiveFrame
	timeout := time.After(time.Second)
	for len(frames) < 2 {
		select {
		case f := <-ch:
			frames = append(frames, f)
		case <-timeout:
			t.Fatalf("frames: %+v", frames)
		}
	}
	if frames[0].Delta != "lo" || !frames[1].Done {
		t.Fatalf("%+v", frames)
	}
	select {
	case f := <-ch:
		t.Fatalf("End is idempotent: %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
}
