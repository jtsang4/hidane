package agents

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func BenchmarkReplyExtractor(b *testing.B) {
	for _, size := range []int{32_000, 128_000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			raw := `{"effects":[{"type":"reply","reply":"` + strings.Repeat("x", size) + `"}]}`
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			for b.Loop() {
				extract := ReplyExtractor()
				for i := 0; i < len(raw); i += 64 {
					extract(raw[i:min(i+64, len(raw))])
				}
			}
		})
	}
}

func TestReplyExtractorStreamsOnlyTheReply(t *testing.T) {
	raw := `{"effects":[{"type":"reply","of":"ev_1","reply":"你好\n世界 \"quoted\" é 😀 done"}]}`
	for _, size := range []int{1, 2, 3, 5, 13, len(raw)} {
		ex := ReplyExtractor()
		var out strings.Builder
		b := []byte(raw)
		for i := 0; i < len(b); {
			j := min(i+size, len(b))
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

func TestReplyExtractorSplitEscapesAndRunes(t *testing.T) {
	raw := `noise {"type":"reply","reply" : "你好\n\t\r\b\f\/\\\"\u00e9\uD83D\uDE00 done","other":"ignored"}`
	want := "你好\n\t\r\b\f/\\\"é😀 done"
	for size := 1; size <= len(raw); size++ {
		extract := ReplyExtractor()
		var out strings.Builder
		for i := 0; i < len(raw); i += size {
			delta := extract(raw[i:min(i+size, len(raw))])
			if !utf8.ValidString(delta) {
				t.Fatalf("size %d emitted a partial rune: %q", size, delta)
			}
			out.WriteString(delta)
		}
		if out.String() != want {
			t.Fatalf("size %d: got %q, want %q", size, out.String(), want)
		}
		if extract(`{"reply":"another"}`) != "" {
			t.Fatal("only the first reply is streamed")
		}
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
