// Package agentcli drives the local coding agent CLIs — Claude Code, Codex and
// pi — as subprocesses. Every role in hidane runs on one of them: reasoning
// roles (Primary, Manager, distiller) without tools, workers with tools behind
// the hidane guard.
package agentcli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/settings"
)

// Image is an attachment stored on disk (the log keeps references, not bytes).
type Image struct {
	Path     string `json:"path"`
	MimeType string `json:"mimeType"`
}

func (im Image) base64() (string, error) {
	b, err := os.ReadFile(im.Path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ToolEvent is a tool call boundary: the two-phase side-effect hook.
type ToolEvent struct {
	Phase   string // start | end
	Tool    string
	Detail  string
	IsError bool
}

// Request is one run.
type Request struct {
	Prompt string
	// SystemPrompt is the role charter, appended to the CLI's own prompt.
	SystemPrompt string
	Cwd          string
	Images       []Image
	// Tools: false for reasoning-only roles.
	Tools    bool
	Model    string
	Effort   string
	Provider *settings.Provider
	// ResumeID continues an earlier native session (Manager continuity).
	ResumeID   string
	SessionDir string
	// Env is extra environment, e.g. the guard's policy files.
	Env     []string
	Timeout time.Duration

	OnText func(delta string)
	OnTool func(ToolEvent)
	// OnSteerConsumed fires when the CLI has taken in steered input.
	OnSteerConsumed func()
}

// Result is how a run ended.
type Result struct {
	OK         bool
	Text       string
	Error      string
	SessionID  string
	ToolCalls  int
	Cancelled  bool
	DurationMs int64
}

// Run is a started CLI process.
type Run interface {
	// Steer hands the running agent new input from the person; false when it
	// can no longer take any.
	Steer(text string) bool
	Cancel()
	Wait() Result
}

// Starter starts runs; the app uses a Launcher, tests may substitute.
type Starter interface {
	Start(ctx context.Context, agent string, req Request) (Run, error)
}

// Launcher starts the configured CLIs.
type Launcher struct {
	// Binary resolves the executable for an agent kind.
	Binary func(agent string) (string, error)
	// GuardCommand is the hidane executable that implements `guard`.
	GuardCommand string
	// RuntimeDir holds generated helper files (pi guard shim).
	RuntimeDir string
	// Env is the base environment for every CLI (resolved PATH, hygiene applied).
	Env []string
}

func (l *Launcher) Start(ctx context.Context, agent string, req Request) (Run, error) {
	bin, err := l.Binary(agent)
	if err != nil {
		return nil, err
	}
	if req.Timeout <= 0 {
		req.Timeout = 10 * time.Minute
	}
	switch agent {
	case settings.Claude:
		return startClaude(ctx, l, bin, req)
	case settings.Codex:
		return startCodex(ctx, l, bin, req)
	case settings.Pi:
		return startPi(ctx, l, bin, req)
	}
	return nil, fmt.Errorf("unknown agent %q", agent)
}

// Call runs one request to completion.
func Call(ctx context.Context, s Starter, agent string, req Request) Result {
	started := time.Now()
	run, err := s.Start(ctx, agent, req)
	if err != nil {
		return Result{Error: err.Error(), DurationMs: time.Since(started).Milliseconds()}
	}
	return run.Wait()
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// effort maps hidane's low/medium/high onto each CLI's own scale.
func effortFor(agent, effort string) string {
	if effort == "" {
		return ""
	}
	switch agent {
	case settings.Pi:
		return effort // off|minimal|low|medium|high|xhigh|max
	default:
		return effort
	}
}
