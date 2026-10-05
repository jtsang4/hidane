package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
)

// Hiding a person's words is a mask every reader applies: the page, the live
// stream and the distiller. Hiding twice records nothing new, and only what a
// person said can be hidden.
func TestRedactHidesWordsFromEveryReader(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	from := m(e.k.LatestSeq(ctx))
	_, body := e.do("POST", "/api/chat", "", map[string]any{"text": "我的密钥是 sk-hunter2"})
	id := body["messageId"].(string)
	if code, body := e.do("POST", "/api/messages/"+id+"/redact", "", nil); code != 200 || body["eventId"] == nil {
		t.Fatalf("redact: %d %v", code, body)
	}
	if code, body := e.do("POST", "/api/messages/"+id+"/redact", "", nil); code != 200 || body["eventId"] != nil {
		t.Fatalf("again: %d %v", code, body)
	}
	if n := len(m(e.k.ListEvents(ctx, kernel.ListFilter{Kind: "message.redacted"}))); n != 1 {
		t.Fatalf("hiding twice records once: %d", n)
	}
	reply := m(e.k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: kernel.Payload{"text": "x", "root": id}}))
	for _, other := range []string{reply.ID, "ev_nope"} {
		if code, _ := e.do("POST", "/api/messages/"+other+"/redact", "", nil); code != 404 {
			t.Fatalf("%s is not a person's message: %d", other, code)
		}
	}

	res, err := http.Get(e.srv.URL + "/api/events?page=1&conversation=1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var page struct{ Events []kernel.Event }
	if err := json.Unmarshal(raw, &page); err != nil || strings.Contains(string(raw), "hunter2") {
		t.Fatalf("the page shows the words: %s", raw)
	}
	masked := false
	for _, ev := range page.Events {
		masked = masked || ev.ID == id && ev.Payload.Bool("redacted") && ev.Payload.Str("text") == ""
	}
	if !masked {
		t.Fatalf("the message stays, masked: %s", raw)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events/stream?after="+strconv.FormatInt(from, 10), nil)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	next := frames(t, ctx, stream.Body)
	streamed := false
	for f := next(); !strings.Contains(f.data, `"message.redacted"`); f = next() {
		if strings.Contains(f.data, "hunter2") {
			t.Fatalf("the stream replays the words: %s", f.data)
		}
		streamed = streamed || strings.Contains(f.data, `"id":"`+id+`"`) && strings.Contains(f.data, `"redacted":true`)
	}
	if !streamed {
		t.Fatal("the stream replays the message masked")
	}
	for _, ev := range agents.MeaningfulEvents(m(e.k.ListEvents(ctx, kernel.ListFilter{}))) {
		if ev.ID == id {
			t.Fatal("the distiller must never read hidden words")
		}
	}
}
