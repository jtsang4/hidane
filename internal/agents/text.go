package agents

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jtsang4/hidane/internal/kernel"
)

var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// ExtractJSON returns the first JSON object in model output (fenced block preferred).
func ExtractJSON(text string, v any) bool {
	var candidates []string
	if m := fencedJSON.FindStringSubmatch(text); m != nil {
		candidates = append(candidates, m[1])
	}
	if start := strings.IndexByte(text, '{'); start >= 0 {
		depth, inStr, esc := 0, false, false
		for i := start; i < len(text); i++ {
			ch := text[i]
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = !inStr
			}
			if inStr {
				continue
			}
			if ch == '{' {
				depth++
			}
			if ch == '}' {
				depth--
				if depth == 0 {
					candidates = append(candidates, text[start:i+1])
					break
				}
			}
		}
	}
	for _, c := range candidates {
		if json.Unmarshal([]byte(c), v) == nil {
			return true
		}
	}
	return false
}

// Effect is one decision in a role's answer.
type Effect = kernel.Payload

// ParseEffects reads `{"effects":[...]}` (or a single effect object), or nil
// when the answer was not the effect JSON the charter asks for.
func ParseEffects(text string) []Effect {
	var parsed map[string]any
	if !ExtractJSON(text, &parsed) {
		return nil
	}
	var list []any
	if l, ok := parsed["effects"].([]any); ok {
		list = l
	} else if _, ok := parsed["type"].(string); ok {
		list = []any{parsed}
	} else {
		return nil
	}
	out := []Effect{}
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if _, ok := m["type"].(string); ok {
				out = append(out, Effect(m))
			}
		}
	}
	return out
}

// Str is a trimmed non-empty string field, or "".
func Str(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// ReplyExtractor turns a role's raw JSON token stream into the human-facing
// `reply` text only. Each byte is consumed once; only an incomplete escape or
// UTF-8 rune is held across chunks, never the accumulated response.
func ReplyExtractor() func(chunk string) string {
	const key = `"reply"`
	matched, phase := 0, 0 // key, colon, opening quote, reply, finished
	pending := ""
	return func(chunk string) string {
		if phase == 4 {
			return ""
		}
		i := 0
		for phase < 3 && i < len(chunk) {
			ch := chunk[i]
			i++
			if phase == 0 && ch == key[matched] {
				matched++
				if matched == len(key) {
					phase = 1
				}
				continue
			}
			if phase > 0 && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f') {
				continue
			}
			if phase == 1 && ch == ':' {
				phase = 2
				continue
			}
			if phase == 2 && ch == '"' {
				phase = 3
				break
			}
			phase, matched = 0, 0
			if ch == '"' {
				matched = 1
			}
		}
		if phase < 3 {
			return ""
		}
		raw := pending + chunk[i:]
		pending = ""
		var out strings.Builder
		i = 0
		for i < len(raw) {
			ch := raw[i]
			if ch == '"' {
				phase = 4
				break
			}
			if ch != '\\' {
				if !utf8.FullRuneInString(raw[i:]) {
					break
				}
				_, size := utf8.DecodeRuneInString(raw[i:])
				out.WriteString(raw[i : i+size])
				i += size
				continue
			}
			// A trailing backslash is the front half of an escape still in flight.
			if i+1 >= len(raw) {
				break
			}
			esc := raw[i+1]
			if esc == 'u' {
				if i+6 > len(raw) {
					break
				}
				hi, err := strconv.ParseUint(raw[i+2:i+6], 16, 32)
				if err != nil {
					phase = 4
					break
				}
				r := rune(hi)
				step := 6
				if utf16.IsSurrogate(r) {
					// Never hand out half a surrogate pair: the two halves can land
					// in separate chunks.
					if i+12 > len(raw) {
						break
					}
					if raw[i+6] != '\\' || raw[i+7] != 'u' {
						phase = 4
						break
					}
					lo, err := strconv.ParseUint(raw[i+8:i+12], 16, 32)
					if err != nil {
						phase = 4
						break
					}
					r = utf16.DecodeRune(r, rune(lo))
					step = 12
				}
				out.WriteRune(r)
				i += step
				continue
			}
			switch esc {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
				out.WriteByte('\r')
			case 'b':
				out.WriteByte('\b')
			case 'f':
				out.WriteByte('\f')
			default:
				out.WriteByte(esc)
			}
			i += 2
		}
		if phase != 4 {
			pending = strings.Clone(raw[i:])
		}
		return out.String()
	}
}

// LiveFrame is a frame of a reply still being written. These are deliberately
// NOT events: the log records facts, not thinking — half a sentence is
// neither. Frames live in this process only and are superseded by the durable
// agent.reply the moment it lands.
type LiveFrame struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	Delta    string `json:"delta,omitempty"`
	Text     string `json:"text,omitempty"`
	Done     bool   `json:"done,omitempty"`
}

type liveStream struct {
	threadID string
	text     strings.Builder
}

// LiveText is the in-process bus of replies being written.
type LiveText struct {
	mu   sync.Mutex
	open map[string]*liveStream
	subs map[chan LiveFrame]struct{}
}

func NewLiveText() *LiveText {
	return &LiveText{open: map[string]*liveStream{}, subs: map[chan LiveFrame]struct{}{}}
}

// A runaway model must not pin unbounded memory: past this the replay
// snapshot stops growing; deltas still flow.
const snapshotCap = 32_000

func (l *LiveText) emit(f LiveFrame) {
	for ch := range l.subs {
		select {
		case ch <- f:
		default:
		}
	}
}

// LiveHandle feeds one reply stream.
type LiveHandle struct {
	l  *LiveText
	id string
}

func (l *LiveText) Begin(threadID string) *LiveHandle {
	id := kernel.GenID("ls", 8)
	l.mu.Lock()
	l.open[id] = &liveStream{threadID: threadID}
	l.mu.Unlock()
	return &LiveHandle{l: l, id: id}
}

func (h *LiveHandle) Push(delta string) {
	if h == nil || delta == "" {
		return
	}
	h.l.mu.Lock()
	defer h.l.mu.Unlock()
	s := h.l.open[h.id]
	if s == nil {
		return
	}
	if s.text.Len() < snapshotCap {
		s.text.WriteString(delta)
	}
	h.l.emit(LiveFrame{ID: h.id, ThreadID: s.threadID, Delta: delta})
}

// End is idempotent, so callers may close defensively.
func (h *LiveHandle) End() {
	if h == nil {
		return
	}
	h.l.mu.Lock()
	defer h.l.mu.Unlock()
	s := h.l.open[h.id]
	if s == nil {
		return
	}
	delete(h.l.open, h.id)
	h.l.emit(LiveFrame{ID: h.id, ThreadID: s.threadID, Done: true})
}

// Subscribe returns the replies already in flight (for a client joining
// mid-run) and a channel of later frames. Taking both under one lock means a
// frame can be neither lost nor counted twice.
func (l *LiveText) Subscribe() ([]LiveFrame, <-chan LiveFrame, func()) {
	ch := make(chan LiveFrame, 256)
	l.mu.Lock()
	var snap []LiveFrame
	for id, s := range l.open {
		if s.text.Len() > 0 {
			snap = append(snap, LiveFrame{ID: id, ThreadID: s.threadID, Text: s.text.String()})
		}
	}
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return snap, ch, func() {
		l.mu.Lock()
		if _, ok := l.subs[ch]; ok {
			delete(l.subs, ch)
			close(ch)
		}
		l.mu.Unlock()
	}
}
