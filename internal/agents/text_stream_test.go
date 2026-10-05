package agents

import (
	"runtime"
	"strings"
	"testing"
)

// A long reply streams in many small pieces; each is read once. An extractor
// that parsed the text received so far on every piece costs time and memory
// in the square of the reply's length, and the right output would not show it.
func TestReplyExtractorReadsOnlyWhatArrives(t *testing.T) {
	const chunk = 64
	raw := `{"effects":[{"type":"reply","of":"ev_1","reply":"` + strings.Repeat(`长段落\n\"引号\" `, 12_000) + `"}]}`
	extract := ReplyExtractor()
	var out strings.Builder
	out.Grow(len(raw))
	half := len(raw) / 2 / chunk * chunk
	for i := 0; i < half; i += chunk {
		out.WriteString(extract(raw[i : i+chunk]))
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := half; i < len(raw); i += chunk {
		out.WriteString(extract(raw[i:min(i+chunk, len(raw))]))
	}
	runtime.ReadMemStats(&after)
	if want := strings.Repeat("长段落\n\"引号\" ", 12_000); out.String() != want {
		t.Fatalf("the reply came out wrong: %d of %d bytes", out.Len(), len(want))
	}
	// Linear: a few bytes per byte read. Re-reading what was received (≥ 130 KB)
	// for each of the second half's ≈ 2,000 pieces would be hundreds of megabytes.
	read := len(raw) - half
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > uint64(16*read) {
		t.Fatalf("reading the second %d bytes allocated %d bytes: the text so far is read again", read, allocated)
	}
}
