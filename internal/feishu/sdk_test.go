package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

// What reaches Feishu's API: rich replies are cards (interactive), plain ones text.
func TestSDKSendsCardsAsInteractive(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"t-test","expire":7200}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["_path"] = r.URL.Path
		mu.Lock()
		sent = append(sent, body)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"code":0,"data":{"message_id":"om_1"}}`)
	}))
	defer srv.Close()
	s := &SDK{client: lark.NewClient("cli_test", "secret", lark.WithOpenBaseUrl(srv.URL))}
	ctx := context.Background()
	if id, err := s.SendText(ctx, "oc_1", "**hi**", true); err != nil || id != "om_1" {
		t.Fatalf("rich send: %q %v", id, err)
	}
	if _, err := s.SendText(ctx, "oc_1", "plain", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplyInThread(ctx, "om_root", "**thread**"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 {
		t.Fatalf("requests: %+v", sent)
	}
	for i, want := range []string{"interactive", "text", "interactive"} {
		if sent[i]["msg_type"] != want {
			t.Fatalf("request %d: msg_type %v, want %s (%+v)", i, sent[i]["msg_type"], want, sent[i])
		}
	}
	var card map[string]any
	if err := json.Unmarshal([]byte(sent[0]["content"].(string)), &card); err != nil || card["schema"] != "2.0" {
		t.Fatalf("card 2.0 content: %v %v", card, err)
	}
	if sent[2]["reply_in_thread"] != true || !strings.Contains(sent[2]["_path"].(string), "om_root/reply") {
		t.Fatalf("thread reply: %+v", sent[2])
	}
}
