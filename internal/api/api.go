// Package api is the one http.Handler behind both the desktop webview (via the
// Wails asset server) and headless `hidane serve`: /api/*, the live frame
// stream, /boot.js, /health, /webhook/:name and the embedded SPA.
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// Options configure one handler.
type Options struct {
	K        *kernel.Kernel
	Sys      *agents.System
	Settings *settings.Store
	// Detect reports which agent CLIs can run.
	Detect func(ctx context.Context) []agentcli.Detection
	// Assets is the built SPA (frontend/dist); nil serves no UI.
	Assets fs.FS
	// Desktop: the only client is the app's own webview, so no token is asked.
	Desktop       bool
	Token         string
	WebhookSecret string
	Version       string
	// OnUIReady is called when the SPA reports its live channel works, with
	// the transport it ended up on (GUI smoke test).
	OnUIReady func(transport string)
	// OnLiveHello re-greets a page that just subscribed to pushed frames.
	OnLiveHello func()
	// OnSettingsChanged drops anything derived from settings (CLI detection).
	OnSettingsChanged func()
	// FireSchedule runs a schedule now.
	FireSchedule func(ctx context.Context, sc kernel.Schedule) (string, error)
}

type server struct {
	Options
	mux *http.ServeMux
}

// New builds the handler.
func New(o Options) http.Handler {
	s := &server{Options: o, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") && !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, errBody("unauthorized"))
		return
	}
	s.mux.ServeHTTP(w, r)
}

func safeEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// authorized: /api/* requires the bearer token whenever one is configured.
// EventSource cannot set headers, so the stream may pass ?token= instead.
func (s *server) authorized(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	} else {
		token = r.URL.Query().Get("token")
	}
	return token != "" && safeEqual(token, s.Token)
}

// SignWebhook is the x-hidane-signature value for a body.
func SignWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errBody(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }

const maxBody = 40 << 20

func readJSON(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

func notFound(err error) bool {
	return errors.Is(err, kernel.ErrNotFound) || errors.Is(err, settings.ErrNotFound)
}

func (s *server) routes() {
	m := s.mux
	m.HandleFunc("GET /health", s.health)
	m.HandleFunc("POST /webhook/{name}", s.webhook)
	m.HandleFunc("GET /boot.js", s.boot)

	m.HandleFunc("GET /api/events", s.events)
	m.HandleFunc("GET /api/events/stream", s.stream)
	m.HandleFunc("GET /api/work-items", s.workItems)
	m.HandleFunc("POST /api/work-items", s.createWorkItem)
	m.HandleFunc("GET /api/work-items/{id}", s.workItem)
	m.HandleFunc("PATCH /api/work-items/{id}", s.patchWorkItem)
	m.HandleFunc("POST /api/work-items/{id}/cancel", s.cancelWorkItem)
	m.HandleFunc("GET /api/work-items/{id}/files", s.files)
	m.HandleFunc("GET /api/work-items/{id}/file", s.file)
	m.HandleFunc("POST /api/chat", s.chat)
	m.HandleFunc("POST /api/messages/{id}/route", s.routeMessage)
	m.HandleFunc("POST /api/messages/{id}/redact", s.redact)
	m.HandleFunc("GET /api/conversation/search", s.search)
	m.HandleFunc("GET /api/conversation/days", s.days)
	m.HandleFunc("GET /api/conversation/context", s.conversationContext)
	m.HandleFunc("GET /api/board", s.board)
	m.HandleFunc("GET /api/policies", s.policies)
	m.HandleFunc("POST /api/policies", s.addPolicy)
	m.HandleFunc("DELETE /api/policies/{id}", s.deletePolicy)
	m.HandleFunc("GET /api/memories", s.memories)
	m.HandleFunc("POST /api/memories", s.addMemory)
	m.HandleFunc("DELETE /api/memories/{id}", s.forgetMemory)
	m.HandleFunc("GET /api/schedules", s.schedules)
	m.HandleFunc("POST /api/schedules", s.createSchedule)
	m.HandleFunc("PATCH /api/schedules/{id}", s.updateSchedule)
	m.HandleFunc("DELETE /api/schedules/{id}", s.deleteSchedule)
	m.HandleFunc("GET /api/schedules/{id}/runs", s.scheduleRuns)
	m.HandleFunc("POST /api/schedules/{id}/run", s.runSchedule)
	m.HandleFunc("GET /api/worklog/{day}", s.worklog)
	m.HandleFunc("GET /api/status", s.status)

	m.HandleFunc("GET /api/settings", s.getSettings)
	m.HandleFunc("PUT /api/settings/roles", s.putRoles)
	m.HandleFunc("PUT /api/settings/binaries", s.putBinaries)
	m.HandleFunc("POST /api/providers", s.addProvider)
	m.HandleFunc("PATCH /api/providers/{id}", s.patchProvider)
	m.HandleFunc("DELETE /api/providers/{id}", s.deleteProvider)
	m.HandleFunc("GET /api/provider-presets", s.presets)
	m.HandleFunc("GET /api/agents", s.agentsList)
	m.HandleFunc("POST /api/agents/test", s.testAgent)
	m.HandleFunc("POST /api/ui-ready", s.uiReady)
	m.HandleFunc("POST /api/live/hello", s.liveHello)

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusNotFound, errBody("not found")) })
	m.HandleFunc("/", s.static)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.K.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "db": "down", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": "up"})
}

// webhook is the passive connector: capture and normalize, never judge.
func (s *server) webhook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	if s.WebhookSecret != "" {
		sig := r.Header.Get("x-hidane-signature")
		if sig == "" || !safeEqual(sig, SignWebhook(raw, s.WebhookSecret)) {
			writeJSON(w, http.StatusUnauthorized, errBody("invalid signature"))
			return
		}
	}
	var body any
	if json.Unmarshal(raw, &body) != nil {
		body = map[string]any{"raw": string(raw)}
	}
	ev, err := s.K.Append(r.Context(), kernel.EventInput{Source: "connector:webhook:" + name, Kind: "connector.webhook",
		Payload: kernel.Payload{"name": name, "body": body}})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "eventId": ev.ID, "seq": ev.Seq})
}

func (s *server) boot(w http.ResponseWriter, r *http.Request) {
	b, _ := json.Marshal(map[string]any{"desktop": s.Desktop, "auth": !s.Desktop && s.Token != "", "version": s.Version})
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, "window.hidaneBoot = %s;\n", b)
}

func (s *server) uiReady(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Transport string `json:"transport"`
	}
	_ = readJSON(r, &body)
	if s.OnUIReady != nil {
		s.OnUIReady(body.Transport)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) liveHello(w http.ResponseWriter, r *http.Request) {
	if s.OnLiveHello != nil {
		s.OnLiveHello()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// static serves the SPA: real files as themselves, every other path the app shell.
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if s.Assets == nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p != "" && p != "index.html" {
		if f, err := s.Assets.Open(p); err == nil {
			info, err := f.Stat()
			f.Close()
			if err == nil && !info.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.ServeFileFS(w, r, s.Assets, p)
				return
			}
		}
		if path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
	}
	index, err := fs.ReadFile(s.Assets, "index.html")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusNotFound)
		return
	}
	// Read per request: a cached shell outlives a rebuild and points deep
	// links at asset hashes that no longer exist.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func has(r *http.Request, key string) bool { _, ok := r.URL.Query()[key]; return ok }

type eventsPage struct {
	Events    []kernel.Event    `json:"events"`
	HasMore   bool              `json:"hasMore"`
	HasNewer  bool              `json:"hasNewer"`
	OldestSeq *int64            `json:"oldestSeq"`
	NewestSeq *int64            `json:"newestSeq"`
	Titles    map[string]string `json:"titles"`
}

func ends(events []kernel.Event) (*int64, *int64) {
	if len(events) == 0 {
		return nil, nil
	}
	a, b := events[0].Seq, events[len(events)-1].Seq
	return &a, &b
}

func nonNil(events []kernel.Event) []kernel.Event {
	if events == nil {
		return []kernel.Event{}
	}
	return events
}

// Pump drives a live frame stream (SSE or desktop events) from an exclusive
// seq; after < 0 starts at the tail.
type Frame struct {
	Event string
	ID    string
	Data  any
}

const (
	pingEvery = 15 * time.Second
	pollEvery = 5 * time.Second
)

// Pump writes `hello`, then durable `hidane` log events and ephemeral
// `stream` reply frames as they happen, and a `ping` whenever the stream has
// been quiet — clients judge liveness by silence, because a dead server does
// not close a stream in a way they can see.
func Pump(ctx context.Context, k *kernel.Kernel, live *agents.LiveText, after int64, send func(Frame) error) error {
	snapshot, frames, cancelLive := live.Subscribe()
	defer cancelLive()
	wake, cancelHub := k.Hub.Subscribe()
	defer cancelHub()
	// Greet before touching the database, so a client never sees a committed
	// response with no body.
	if err := send(Frame{Event: "hello", Data: map[string]any{"after": after}}); err != nil {
		return err
	}
	for _, f := range snapshot {
		if err := send(Frame{Event: "stream", Data: f}); err != nil {
			return err
		}
	}
	cursor := after
	lastWrite := time.Now()
	for {
		if cursor < 0 {
			seq, err := k.LatestSeq(ctx)
			if err == nil {
				cursor = seq
			}
		}
		if cursor >= 0 {
			fresh, err := k.ListEvents(ctx, kernel.ListFilter{AfterSeq: &cursor, Limit: 100})
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			for _, e := range fresh {
				cursor = e.Seq
				if err := send(Frame{Event: "hidane", ID: strconv.FormatInt(e.Seq, 10), Data: e}); err != nil {
					return err
				}
				lastWrite = time.Now()
			}
			if len(fresh) == 100 {
				continue
			}
		}
		if time.Since(lastWrite) >= pingEvery {
			if err := send(Frame{Event: "ping", Data: time.Now().UnixMilli()}); err != nil {
				return err
			}
			lastWrite = time.Now()
		}
		timer := time.NewTimer(pollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case f, ok := <-frames:
			timer.Stop()
			if !ok {
				return nil
			}
			// A delta is worth less the later it lands: drain what is queued.
			if err := send(Frame{Event: "stream", Data: f}); err != nil {
				return err
			}
			for drained := false; !drained; {
				select {
				case more := <-frames:
					if err := send(Frame{Event: "stream", Data: more}); err != nil {
						return err
					}
				default:
					drained = true
				}
			}
			lastWrite = time.Now()
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// ResumeCursor is where a stream should resume from, as an exclusive seq: an
// explicit `after` wins, otherwise the browser's Last-Event-ID. Values that
// are not whole non-negative numbers mean "from the tail" (-1) — `""` would
// otherwise read as 0 and replay the whole log.
func ResumeCursor(after, lastEventID string) int64 {
	for _, c := range []string{after, lastEventID} {
		if strings.TrimSpace(c) == "" {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(c), 10, 64)
		if err == nil && n >= 0 {
			return n
		}
		return -1
	}
	return -1
}

func (s *server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errBody("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	after := ResumeCursor(r.URL.Query().Get("after"), r.Header.Get("Last-Event-ID"))
	_ = Pump(r.Context(), s.K, s.Sys.Live, after, func(f Frame) error {
		b, err := json.Marshal(f.Data)
		if err != nil {
			return err
		}
		if f.ID != "" {
			fmt.Fprintf(w, "id: %s\n", f.ID)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", f.Event, b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
}
