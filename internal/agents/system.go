// Package agents is the runtime's three roles — Primary, Manager, Worker — as
// one loop at three scopes, plus the memory distiller. Every role runs on a
// local agent CLI chosen in settings.
package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/guard"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/repos"
	"github.com/jtsang4/hidane/internal/settings"
)

// System wires the roles to the kernel, the settings and the CLIs.
type System struct {
	K        *kernel.Kernel
	Settings *settings.Store
	Agents   agentcli.Starter
	Live     *LiveText
	Pool     *WorkerPool
	Runtime  *kernel.Runtime
	Repos    *repos.Service

	// statusMu makes a status change and the look at its siblings one step:
	// two children finishing at once each saw the other done, and their
	// parent was told twice that all had settled.
	statusMu sync.Mutex
}

func New(k *kernel.Kernel, st *settings.Store, starter agentcli.Starter) *System {
	s := &System{K: k, Settings: st, Agents: starter, Live: NewLiveText(), Repos: repos.New(k)}
	s.Pool = newWorkerPool(s)
	return s
}

// Thought is one model call's outcome.
type Thought struct {
	OK         bool
	Effects    []Effect
	Raw        string
	Error      string
	DurationMs int64
	SessionID  string
	// Aborted: the runtime is stopping; the turn must not act on this.
	Aborted bool
}

// decision is the payload of route.decision / manager.decision: what a turn
// over the messages `of` decided.
func (th Thought) decision(of []kernel.Event) kernel.Payload {
	ids := []any{}
	for _, m := range of {
		ids = append(ids, m.ID)
	}
	recorded := []any{}
	for _, e := range th.Effects {
		recorded = append(recorded, map[string]any(e))
	}
	return kernel.Payload{"ok": th.OK, "durationMs": th.DurationMs, "of": ids, "effects": recorded}
}

type thinkOpts struct {
	Role string
	// Own is a work item's own choice of agent, in place of the role's.
	Own          *settings.RoleConfig
	Charter      string
	Cwd          string
	SessionDir   string
	Images       []agentcli.Image
	LiveThreadID string
	ResumeID     string
	// Tools hands the role the CLI's own tools in its bypass mode, behind the
	// guard: it may look things up and change files itself. Its charter still
	// replaces the CLI's prompt, so it answers with its effect list.
	Tools       bool
	PolicyFiles []string
	// Trail says where the role's tool calls are recorded as side effects.
	Trail kernel.EventInput
}

// think is the shared half of every agent loop: one model call over the
// turn's context, streamed to a thread as it is written, parsed into effects.
// Roles differ in the context they build and the effects they may emit.
func (s *System) think(ctx context.Context, prompt string, o thinkOpts) Thought {
	r := s.Settings.Get().ResolveWith(o.Role, o.Own)
	// The provisional bubble closes when the model stops, not when effects are
	// applied: the durable reply takes over from it.
	var live *LiveHandle
	var extract func(string) string
	if o.LiveThreadID != "" {
		live = s.Live.Begin(o.LiveThreadID)
		extract = ReplyExtractor()
	}
	_ = os.MkdirAll(o.Cwd, 0o755)
	req := agentcli.Request{
		Prompt:       prompt,
		SystemPrompt: o.Charter,
		Cwd:          o.Cwd,
		Images:       o.Images,
		Model:        r.Model,
		Effort:       r.Effort,
		Provider:     r.Provider,
		ResumeID:     o.ResumeID,
		SessionDir:   o.SessionDir,
		Timeout:      s.K.Cfg.RouteTimeout,
	}
	if live != nil {
		req.OnText = func(d string) { live.Push(extract(d)) }
	}
	var trail *toolTrail
	if o.Tools {
		req.Tools, req.ReplacePrompt = true, true
		// A refusal of the role's own call is a fact like a worker's.
		control := filepath.Join(s.K.Cfg.RuntimeDir(), "turns", kernel.GenID("turn", 8))
		_ = os.MkdirAll(control, 0o755)
		defer os.RemoveAll(control)
		blocks := filepath.Join(control, "policy-blocks.jsonl")
		req.Env = guard.Env{PolicyFiles: o.PolicyFiles, BlocksFile: blocks}.Vars()
		trail = newToolTrail(s.K, o.Trail, nil, blocks)
		req.OnTool = trail.tool
		defer trail.watch()()
	}
	res := agentcli.Call(ctx, s.Agents, r.Agent, req)
	if trail != nil {
		trail.finish()
	}
	live.End()
	if ctx.Err() != nil {
		return Thought{Error: ctx.Err().Error(), Aborted: true, DurationMs: res.DurationMs}
	}
	if !res.OK {
		return Thought{Error: res.Error, DurationMs: res.DurationMs, SessionID: res.SessionID}
	}
	return Thought{OK: true, Effects: ParseEffects(res.Text), Raw: res.Text, DurationMs: res.DurationMs, SessionID: res.SessionID}
}

// roleDir is a reasoning role's empty working directory. Its cwd leaks into
// what a model writes; the data directory itself must never become a brief's
// "workspace".
func roleDir(k *kernel.Kernel, role string) string {
	dir := filepath.Join(k.Cfg.RuntimeDir(), "roles", role)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// Ping makes one real round trip for a role — the only proof that an agent,
// provider, model and key actually work together.
func (s *System) Ping(ctx context.Context, role string) (agentcli.Result, settings.Resolved) {
	r := s.Settings.Get().Resolve(role)
	dir := roleDir(s.K, "ping")
	req := agentcli.Request{
		Prompt: "ping", SystemPrompt: PingCharter, Cwd: dir, Model: r.Model, Provider: r.Provider,
		SessionDir: filepath.Join(dir, "sessions"), Timeout: 150 * time.Second,
	}
	return agentcli.Call(ctx, s.Agents, r.Agent, req), r
}

// Inbound image from a channel, base64 in transit.
type InboundImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

var imageExt = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}

// StoreImages: images wait in files until the turn that reads them. The log
// stores the reference; base64 in a payload would bloat every query.
func (s *System) StoreImages(images []InboundImage) ([]agentcli.Image, error) {
	if len(images) == 0 {
		return nil, nil
	}
	dir := filepath.Join(s.K.Cfg.Home, "inbox", s.K.Today())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var out []agentcli.Image
	for _, im := range images {
		b, err := base64.StdEncoding.DecodeString(im.Data)
		if err != nil {
			continue
		}
		ext := imageExt[im.MimeType]
		if ext == "" {
			ext = "bin"
		}
		path := filepath.Join(dir, kernel.GenID("img", 8)+"."+ext)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return nil, err
		}
		out = append(out, agentcli.Image{Path: path, MimeType: im.MimeType})
	}
	return out, nil
}

// StoredImages reads image references from a payload; missing files are dropped.
func StoredImages(p kernel.Payload) []agentcli.Image {
	raw, ok := p["images"].([]any)
	if !ok {
		return nil
	}
	var out []agentcli.Image
	for _, r := range raw {
		b, _ := json.Marshal(r)
		var im agentcli.Image
		if json.Unmarshal(b, &im) == nil && im.Path != "" && im.MimeType != "" {
			if _, err := os.Stat(im.Path); err == nil {
				out = append(out, im)
			}
		}
	}
	return out
}

func imagesOf(events []kernel.Event) []agentcli.Image {
	var out []agentcli.Image
	for _, e := range events {
		out = append(out, StoredImages(e.Payload)...)
	}
	return out
}

// maxAnswerRunes bounds what an answer or a worker's summary may store. Far
// beyond any real answer: it guards the log, it does not shape replies — a
// silent cut once left the person reading half an answer as if it were whole.
const maxAnswerRunes = 200_000

// clipNoted is clipRunes that says so: a reader must know text was left out.
func clipNoted(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + fmt.Sprintf("\n\n[… %d more characters not shown]", len(r)-n)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func joinNonEmpty(parts []string, sep string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// originOf marks a work item's later replies the way the Primary marks its own
// answers (markOrigin).
func (s *System) originOf(ctx context.Context, rootID string, payload kernel.Payload) {
	if root, ok, err := s.K.GetEvent(ctx, rootID); err == nil && ok {
		markOrigin(root, payload)
	}
}
