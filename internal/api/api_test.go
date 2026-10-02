package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/settings"
)

type stubStarter struct{}

func (stubStarter) Start(context.Context, string, agentcli.Request) (agentcli.Run, error) {
	return nil, io.ErrUnexpectedEOF
}

type env struct {
	t   *testing.T
	k   *kernel.Kernel
	s   *agents.System
	st  *settings.Store
	srv *httptest.Server
}

func newEnv(t *testing.T, o api.Options) *env {
	t.Helper()
	k := kerneltest.New(t)
	st, err := settings.Load(k.Cfg.SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	s := agents.New(k, st, stubStarter{})
	o.K, o.Sys, o.Settings = k, s, st
	if o.Detect == nil {
		o.Detect = func(context.Context) []agentcli.Detection {
			return []agentcli.Detection{{Kind: "claude", Available: true}}
		}
	}
	if o.Assets == nil {
		o.Assets = fstest.MapFS{
			"index.html":    {Data: []byte("<!doctype html><title>hidane</title>")},
			"assets/app.js": {Data: []byte("console.log(1)")},
			"favicon.svg":   {Data: []byte("<svg/>")},
		}
	}
	srv := httptest.NewServer(api.New(o))
	t.Cleanup(srv.Close)
	return &env{t: t, k: k, s: s, st: st, srv: srv}
}

func (e *env) do(method, path, token string, body any) (int, map[string]any) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestAuthGatesTheAPIButNotHealth(t *testing.T) {
	e := newEnv(t, api.Options{Token: "s3cret"})
	if code, _ := e.do("GET", "/api/status", "", nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := e.do("GET", "/api/status", "wrong", nil); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
	if code, _ := e.do("GET", "/api/status", "s3cret", nil); code != 200 {
		t.Fatalf("right token: %d", code)
	}
	if code, body := e.do("GET", "/health", "", nil); code != 200 || body["db"] != "up" {
		t.Fatalf("health: %d %v", code, body)
	}
	res, err := http.Get(e.srv.URL + "/api/events/stream?token=s3cret")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream with query token: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	// Elsewhere a token in the URL is refused: it would end up in logs and history.
	plain, err := http.Get(e.srv.URL + "/api/events?token=s3cret")
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != 401 {
		t.Fatalf("query token outside the stream: %d", plain.StatusCode)
	}
}

func TestBootTellsTheSPAItsMode(t *testing.T) {
	for _, c := range []struct {
		o    api.Options
		want string
	}{
		{api.Options{Desktop: true}, `"auth":false,"desktop":true`},
		{api.Options{Token: "x"}, `"auth":true,"desktop":false`},
	} {
		e := newEnv(t, c.o)
		res, _ := http.Get(e.srv.URL + "/boot.js")
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.Contains(string(b), c.want) || !strings.HasPrefix(string(b), "window.hidaneBoot = ") {
			t.Fatalf("boot.js: %s", b)
		}
	}
}

func TestWebhooksStayClosedWithoutASecret(t *testing.T) {
	e := newEnv(t, api.Options{})
	res, err := http.Post(e.srv.URL+"/webhook/x", "text/plain", strings.NewReader(`{"prompt":"run something"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("an unsigned webhook must be refused: %d", res.StatusCode)
	}
	if n := len(m(e.k.ListEvents(context.Background(), kernel.ListFilter{Kind: "connector.webhook"}))); n != 0 {
		t.Fatal("nothing recorded")
	}
}

func TestWebhookSignature(t *testing.T) {
	e := newEnv(t, api.Options{WebhookSecret: "hook"})
	body := `{"hello":"world"}`
	post := func(sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/webhook/github", strings.NewReader(body))
		if sig != "" {
			req.Header.Set("x-hidane-signature", sig)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if post("") != 401 || post("sha256=bad") != 401 {
		t.Fatal("unsigned and badly signed webhooks are refused")
	}
	if n := len(m(e.k.ListEvents(context.Background(), kernel.ListFilter{Kind: "connector.webhook"}))); n != 0 {
		t.Fatalf("a refused webhook must not be recorded: %d", n)
	}
	if post(api.SignWebhook([]byte(body), "hook")) != 200 {
		t.Fatal("a signed webhook is accepted")
	}
	evs := m(e.k.ListEvents(context.Background(), kernel.ListFilter{Kind: "connector.webhook"}))
	if len(evs) != 1 || evs[0].Source != "connector:webhook:github" {
		t.Fatalf("%+v", evs)
	}
}

func m[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestEventPagination(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx := context.Background()
	var ids []string
	for i := 0; i < 10; i++ {
		ev := m(e.k.Append(ctx, kernel.EventInput{Source: "connector:web", Kind: "user.message", ThreadID: "main", Payload: kernel.Payload{"text": "m"}}))
		ids = append(ids, ev.ID)
		m(e.k.Append(ctx, kernel.EventInput{Source: "connector:timer", Kind: "connector.heartbeat"}))
	}
	_, page := e.do("GET", "/api/events?page=1&conversation=1&limit=4", "", nil)
	events := page["events"].([]any)
	if len(events) != 4 || page["hasMore"] != true {
		t.Fatalf("newest page: %v", page)
	}
	oldest := page["oldestSeq"].(float64)
	_, older := e.do("GET", "/api/events?conversation=1&limit=4&before="+itoa(oldest), "", nil)
	if len(older["events"].([]any)) != 4 {
		t.Fatalf("older page: %v", older)
	}
	_, around := e.do("GET", "/api/events?page=1&conversation=1&limit=4&around="+ids[5], "", nil)
	got := around["events"].([]any)
	found := false
	for _, ev := range got {
		if ev.(map[string]any)["id"] == ids[5] {
			found = true
		}
	}
	if !found || around["hasMore"] != true || around["hasNewer"] != true {
		t.Fatalf("around: %v", around)
	}
	if code, _ := e.do("GET", "/api/events?page=1&after=abc", "", nil); code != 400 {
		t.Fatal("a malformed after is the caller's error")
	}
	if code, _ := e.do("GET", "/api/events?page=1&around=ev_missing", "", nil); code != 404 {
		t.Fatal("around an unknown event")
	}
	_, legacy := e.do("GET", "/api/events?kind=connector.heartbeat,user.message&tail=3", "", nil)
	if len(legacy["events"].([]any)) != 3 {
		t.Fatalf("comma-separated kinds: %v", legacy)
	}
}

func itoa(f float64) string { return strconv.FormatInt(int64(f), 10) }

func TestChatIsTheOneDoor(t *testing.T) {
	e := newEnv(t, api.Options{})
	code, body := e.do("POST", "/api/chat", "", map[string]any{"text": "  do a thing  "})
	if code != 202 || body["messageId"] == nil {
		t.Fatalf("%d %v", code, body)
	}
	pending := m(e.k.PendingMessages(context.Background(), kernel.Primary, 10))
	if len(pending) != 1 || pending[0].Payload.Str("text") != "do a thing" {
		t.Fatalf("delivered to the Primary: %+v", pending)
	}
	if code, _ := e.do("POST", "/api/chat", "", map[string]any{"text": "  "}); code != 400 {
		t.Fatal("empty message")
	}
	if code, _ := e.do("POST", "/api/chat", "", map[string]any{"text": "x", "target": "wi_nope"}); code != 404 {
		t.Fatal("unknown target")
	}
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	code, body = e.do("POST", "/api/chat", "", map[string]any{"images": []any{map[string]any{"data": png, "mimeType": "image/png"}}})
	if code != 202 {
		t.Fatalf("image-only: %d %v", code, body)
	}
	msg, _, _ := e.k.GetEvent(context.Background(), body["messageId"].(string))
	if msg.Payload.Str("text") != api.ImageOnlyText || msg.Payload["imageCount"] != float64(1) {
		t.Fatalf("image-only message: %+v", msg.Payload)
	}
	images := agents.StoredImages(msg.Payload)
	if len(images) != 1 {
		t.Fatalf("the image waits in a file: %+v", msg.Payload)
	}
}

func TestWorkItemEndpoints(t *testing.T) {
	e := newEnv(t, api.Options{})
	code, body := e.do("POST", "/api/work-items", "", map[string]any{"title": "write docs", "brief": "please write docs"})
	if code != 201 || body["dispatched"] != true {
		t.Fatalf("%d %v", code, body)
	}
	id := body["item"].(map[string]any)["id"].(string)
	ws := body["item"].(map[string]any)["workspace"].(string)
	_ = os.WriteFile(filepath.Join(ws, "notes.md"), []byte("# notes"), 0o644)
	if code, body := e.do("GET", "/api/work-items/"+id+"/files", "", nil); code != 200 || len(body["files"].([]any)) != 1 {
		t.Fatalf("files: %d %v", code, body)
	}
	if code, body := e.do("GET", "/api/work-items/"+id+"/file?path=notes.md", "", nil); code != 200 || body["text"] != "# notes" {
		t.Fatalf("file: %d %v", code, body)
	}
	if code, _ := e.do("GET", "/api/work-items/"+id+"/file?path=../../../etc/passwd", "", nil); code != 403 {
		t.Fatal("escaping the workspace is forbidden")
	}
	if code, _ := e.do("PATCH", "/api/work-items/"+id, "", map[string]any{"status": "bogus"}); code != 400 {
		t.Fatal("invalid status")
	}
	if code, body := e.do("PATCH", "/api/work-items/"+id, "", map[string]any{"status": "done"}); code != 200 || body["item"].(map[string]any)["status"] != "done" {
		t.Fatalf("patch: %d %v", code, body)
	}
	if code, _ := e.do("POST", "/api/work-items/"+id+"/cancel", "", nil); code != 409 {
		t.Fatal("nothing to cancel")
	}
	if code, _ := e.do("GET", "/api/work-items/wi_none", "", nil); code != 404 {
		t.Fatal("unknown item")
	}
	if code, body := e.do("GET", "/api/work-items?all", "", nil); code != 200 || len(body["items"].([]any)) != 1 {
		t.Fatalf("list: %v", body)
	}
}

func TestSettingsNeverReturnAKey(t *testing.T) {
	e := newEnv(t, api.Options{})
	code, body := e.do("POST", "/api/providers", "", map[string]any{"label": "DeepSeek", "anthropicBaseUrl": "https://api.deepseek.com/anthropic/",
		"piProvider": "deepseek", "apiKey": "sk-verysecret-9876", "models": []string{"deepseek-v4-pro", " ", "deepseek-v4-pro"}})
	if code != 201 {
		t.Fatalf("%d %v", code, body)
	}
	p := body["provider"].(map[string]any)
	if p["id"] != "deepseek" || p["hasApiKey"] != true || p["apiKeyHint"] != "…9876" || p["anthropicBaseUrl"] != "https://api.deepseek.com/anthropic" {
		t.Fatalf("view: %v", p)
	}
	if len(p["models"].([]any)) != 1 {
		t.Fatalf("models are cleaned: %v", p["models"])
	}
	res, _ := http.Get(e.srv.URL + "/api/settings")
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(raw), "verysecret") {
		t.Fatal("the key must never leave the backend")
	}
	if code, body := e.do("PATCH", "/api/providers/deepseek", "", map[string]any{"label": "DS"}); code != 200 || body["provider"].(map[string]any)["hasApiKey"] != true {
		t.Fatalf("an omitted key is kept: %v", body)
	}
	if e.st.Get().Providers[0].APIKey != "sk-verysecret-9876" {
		t.Fatal("key changed")
	}
	code, body = e.do("PUT", "/api/settings/roles", "", map[string]any{"roles": map[string]any{"worker": map[string]any{"agent": "codex", "provider": "deepseek"}}})
	if code != 400 || !strings.Contains(body["error"].(string), "OpenAI") {
		t.Fatalf("incompatible role: %d %v", code, body)
	}
	if code, _ := e.do("PUT", "/api/settings/roles", "", map[string]any{"roles": map[string]any{"worker": map[string]any{"agent": "pi", "provider": "deepseek", "model": "deepseek-v4-pro", "effort": "high"}}}); code != 200 {
		t.Fatal("compatible role")
	}
	if code, body := e.do("DELETE", "/api/providers/deepseek", "", nil); code != 409 || !strings.Contains(body["error"].(string), "worker") {
		t.Fatalf("in use: %d %v", code, body)
	}
	if code, body := e.do("PATCH", "/api/providers/deepseek", "", map[string]any{"apiKey": ""}); code != 200 || body["provider"].(map[string]any)["hasApiKey"] != false {
		t.Fatalf("an empty key clears it: %v", body)
	}
	if code, _ := e.do("PUT", "/api/settings/binaries", "", map[string]any{"binaries": map[string]any{"claude": "relative/path"}}); code != 400 {
		t.Fatal("binary paths must be absolute")
	}
	info, _ := os.Stat(e.st.Path())
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings.json holds keys and must be private: %v", info.Mode())
	}
	for _, ev := range m(e.k.ListEvents(context.Background(), kernel.ListFilter{Kind: "settings.updated"})) {
		b, _ := json.Marshal(ev.Payload)
		if strings.Contains(string(b), "verysecret") {
			t.Fatal("no key in the log")
		}
	}
	if _, body := e.do("GET", "/api/provider-presets", "", nil); len(body["presets"].([]any)) < 5 {
		t.Fatal("presets")
	}
	if code, body := e.do("POST", "/api/agents/test", "", map[string]any{"role": "nobody"}); code != 400 {
		t.Fatalf("unknown role: %v", body)
	}
}

func TestPoliciesMemoriesSchedules(t *testing.T) {
	fired := 0
	e := newEnv(t, api.Options{FireSchedule: func(ctx context.Context, sc kernel.Schedule) (string, error) { fired++; return "posted", nil }})
	if code, _ := e.do("POST", "/api/policies", "", map[string]any{"pattern": "(", "reason": "x"}); code != 400 {
		t.Fatal("invalid regex")
	}
	code, body := e.do("POST", "/api/policies", "", map[string]any{"pattern": `rm\s`, "reason": "no rm"})
	if code != 201 {
		t.Fatal(code)
	}
	rule := body["rule"].(map[string]any)["id"].(string)
	if _, body := e.do("GET", "/api/policies", "", nil); len(body["rules"].([]any)) != 1 {
		t.Fatal("listed")
	}
	if code, _ := e.do("DELETE", "/api/policies/"+rule, "", nil); code != 200 {
		t.Fatal("deleted")
	}
	if code, _ := e.do("DELETE", "/api/policies/"+rule, "", nil); code != 404 {
		t.Fatal("deleted twice")
	}
	_ = os.WriteFile(e.k.GlobalPolicyPath(), []byte(`{"rules":[`), 0o644)
	if _, body := e.do("GET", "/api/policies", "", nil); body["error"] == nil {
		t.Fatalf("an unreadable policy file is reported, not shown as empty: %v", body)
	}

	code, body = e.do("POST", "/api/memories", "", map[string]any{"kind": "preference", "content": "short answers"})
	if code != 201 {
		t.Fatal(code)
	}
	mem := body["entry"].(map[string]any)["id"].(string)
	if _, body := e.do("GET", "/api/memories", "", nil); len(body["entries"].([]any)) != 1 {
		t.Fatal("memory listed")
	}
	if code, _ := e.do("DELETE", "/api/memories/"+mem, "", nil); code != 200 {
		t.Fatal("forgot")
	}
	if code, _ := e.do("DELETE", "/api/memories/"+mem, "", nil); code != 404 {
		t.Fatal("forget twice")
	}

	if code, body := e.do("POST", "/api/schedules", "", map[string]any{"name": "x", "action": "prompt", "spec": map[string]any{"prompt": "hi"}}); code != 400 || body["error"] == nil {
		t.Fatalf("timing required: %d %v", code, body)
	}
	code, body = e.do("POST", "/api/schedules", "", map[string]any{"name": "daily", "action": "prompt", "intervalSec": 60, "spec": map[string]any{"prompt": "hi"}})
	if code != 201 {
		t.Fatalf("%d %v", code, body)
	}
	id := body["schedule"].(map[string]any)["id"].(string)
	if code, body := e.do("PATCH", "/api/schedules/"+id, "", map[string]any{"cron": "0 9 * * *", "intervalSec": nil}); code != 200 || body["schedule"].(map[string]any)["intervalSec"] != nil {
		t.Fatalf("switch to cron: %d %v", code, body)
	}
	if code, body := e.do("POST", "/api/schedules/"+id+"/run", "", nil); code != 200 || body["status"] != "posted" || fired != 1 {
		t.Fatalf("run now: %d %v", code, body)
	}
	if code, body := e.do("GET", "/api/schedules/"+id+"/runs", "", nil); code != 200 || len(body["runs"].([]any)) < 2 {
		t.Fatalf("runs: %v", body)
	}
	if code, _ := e.do("DELETE", "/api/schedules/"+id, "", nil); code != 200 {
		t.Fatal("deleted")
	}
	if code, _ := e.do("PATCH", "/api/schedules/"+id, "", map[string]any{"name": "y"}); code != 404 {
		t.Fatal("unknown schedule")
	}
	if code, _ := e.do("GET", "/api/worklog/yesterday", "", nil); code != 400 {
		t.Fatal("bad day")
	}
	if code, body := e.do("GET", "/api/worklog/today", "", nil); code != 200 || !strings.HasPrefix(body["markdown"].(string), "# Worklog") {
		t.Fatalf("worklog: %v", body)
	}
}

func TestStreamDeliversHelloLogEventsAndLiveText(t *testing.T) {
	e := newEnv(t, api.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	frames := make(chan [2]string, 16)
	go func() {
		sc := bufio.NewScanner(res.Body)
		var event string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				frames <- [2]string{event, strings.TrimPrefix(line, "data: ")}
			}
		}
	}()
	next := func() [2]string {
		select {
		case f := <-frames:
			return f
		case <-ctx.Done():
			t.Fatal("no frame")
		}
		return [2]string{}
	}
	if f := next(); f[0] != "hello" {
		t.Fatalf("first frame: %v", f)
	}
	time.Sleep(100 * time.Millisecond)
	ev := m(e.k.Append(context.Background(), kernel.EventInput{Source: "test", Kind: "x.y", Payload: kernel.Payload{"a": 1}}))
	f := next()
	if f[0] != "hidane" || !strings.Contains(f[1], ev.ID) {
		t.Fatalf("log frame: %v", f)
	}
	h := e.s.Live.Begin("main")
	h.Push("partial")
	f = next()
	if f[0] != "stream" || !strings.Contains(f[1], "partial") {
		t.Fatalf("live frame: %v", f)
	}
	h.End()
}

func TestResumeCursor(t *testing.T) {
	cases := []struct {
		after, last string
		want        int64
	}{
		{"", "", -1}, {"5", "", 5}, {"", "7", 7}, {"5", "7", 5}, {"x", "7", -1}, {"", "-3", -1}, {" ", "9", 9},
	}
	for _, c := range cases {
		if got := api.ResumeCursor(c.after, c.last); got != c.want {
			t.Errorf("ResumeCursor(%q,%q)=%d want %d", c.after, c.last, got, c.want)
		}
	}
}

func TestStaticServesTheSPA(t *testing.T) {
	e := newEnv(t, api.Options{})
	get := func(path string) (int, string) {
		res, err := http.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	for _, p := range []string{"/", "/items", "/settings?x=1"} {
		if code, body := get(p); code != 200 || !strings.Contains(body, "<title>hidane</title>") {
			t.Fatalf("%s: %d", p, code)
		}
	}
	if code, body := get("/assets/app.js"); code != 200 || body != "console.log(1)" {
		t.Fatalf("asset: %d %q", code, body)
	}
	if code, _ := get("/assets/missing.js"); code != 404 {
		t.Fatal("a missing asset is a 404, not the shell")
	}
	if code, _ := get("/api/nope"); code != 404 {
		t.Fatal("unknown api")
	}
	for _, path := range []string{"/feishu/events", "/random/xyz", "/"} {
		res, err := http.Post(e.srv.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("POST %s must be 404, not the app shell: %d", path, res.StatusCode)
		}
	}
}

type fakeHost struct{ calls []string }

func (h *fakeHost) Open(target string, reveal bool) error {
	h.calls = append(h.calls, fmt.Sprintf("open %v:%s", reveal, target))
	return nil
}
func (h *fakeHost) CopyText(text string) error { h.calls = append(h.calls, "copy "+text); return nil }
func (h *fakeHost) Notify(title, body string) error {
	h.calls = append(h.calls, "notify "+title+"|"+body)
	return nil
}
func (h *fakeHost) SetBadge(n int) error {
	h.calls = append(h.calls, fmt.Sprintf("badge %d", n))
	return nil
}

func TestDesktopHostIsReachableFromTheWebviewButNeverFromServe(t *testing.T) {
	host := &fakeHost{}
	serve := newEnv(t, api.Options{Token: "t", Host: host})
	for _, path := range []string{"/api/desktop/open-url", "/api/desktop/clipboard", "/api/desktop/notify", "/api/desktop/badge", "/api/desktop/open-data-dir"} {
		if code, _ := serve.do("POST", path, "t", map[string]any{"url": "https://example.com", "text": "x", "title": "x", "count": 1}); code != 404 {
			t.Fatalf("serve mode must not act on the host: %s", path)
		}
	}
	e := newEnv(t, api.Options{Desktop: true, Host: host})
	if code, _ := e.do("POST", "/api/desktop/open-url", "", map[string]any{"url": "https://example.com/a?b=1"}); code != 200 {
		t.Fatal("http link")
	}
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "/relative", "https://"} {
		if code, _ := e.do("POST", "/api/desktop/open-url", "", map[string]any{"url": bad}); code != 400 {
			t.Fatalf("%s must be refused", bad)
		}
	}
	item := m(e.k.CreateWorkItem(context.Background(), "x", "test", kernel.CreateWorkItemOpts{}))
	_ = os.WriteFile(filepath.Join(item.Workspace, "out.txt"), []byte("x"), 0o644)
	if code, _ := e.do("POST", "/api/work-items/"+item.ID+"/reveal", "", map[string]any{"path": "out.txt"}); code != 200 {
		t.Fatal("reveal a workspace file")
	}
	if code, _ := e.do("POST", "/api/work-items/"+item.ID+"/reveal", "", map[string]any{"path": "../../../etc/passwd"}); code != 403 {
		t.Fatal("reveal must stay inside the workspace")
	}
	e.do("POST", "/api/desktop/clipboard", "", map[string]any{"text": "hello"})
	if code, _ := e.do("POST", "/api/desktop/notify", "", map[string]any{"title": " "}); code != 400 {
		t.Fatal("a notification needs a title")
	}
	e.do("POST", "/api/desktop/notify", "", map[string]any{"title": "完成", "body": "写好了"})
	e.do("POST", "/api/desktop/badge", "", map[string]any{"count": -3})
	e.do("POST", "/api/desktop/open-data-dir", "", nil)
	want := []string{"open false:https://example.com/a?b=1", "open true:", "copy hello", "notify 完成|写好了", "badge 0", "open false:" + e.k.Cfg.Home}
	if len(host.calls) != len(want) {
		t.Fatalf("calls: %v", host.calls)
	}
	for i, w := range want {
		if !strings.HasPrefix(host.calls[i], w) {
			t.Fatalf("call %d: %q want prefix %q", i, host.calls[i], w)
		}
	}
}
