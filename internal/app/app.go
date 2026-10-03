// Package app assembles hidane: storage, settings, agent CLIs, the agent
// loop and the connectors, for the desktop shell and for `hidane serve`.
package app

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jtsang4/hidane/frontend"
	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/connectors"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// Version is set at build time.
var Version = "dev"

type App struct {
	Cfg      *config.Config
	K        *kernel.Kernel
	Settings *settings.Store
	Sys      *agents.System
	Launcher *agentcli.Launcher
	Runtime  *kernel.Runtime

	release func()
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	detectMu   sync.Mutex
	detectedAt time.Time
	detected   []agentcli.Detection
	catalogs   map[string]cachedCatalog
}

type Options struct {
	// LoginShell resolves PATH from the user's login shell (GUI launches).
	LoginShell bool
}

// Open prepares everything without starting background work.
func Open(cfg *config.Config, o Options) (*App, error) {
	k, err := kernel.Open(cfg)
	if err != nil {
		return nil, err
	}
	st, err := settings.Load(cfg.SettingsPath())
	if err != nil {
		k.Close()
		return nil, err
	}
	self, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			self = resolved
		}
	}
	a := &App{Cfg: cfg, K: k, Settings: st}
	// HIDANE_LOGIN_SHELL=0 keeps tests hermetic: never source a developer's
	// shell startup files to find the CLIs.
	env := agentcli.BaseEnv(o.LoginShell && os.Getenv("HIDANE_LOGIN_SHELL") != "0")
	a.Launcher = &agentcli.Launcher{
		Binary:       a.binary,
		GuardCommand: self,
		RuntimeDir:   cfg.RuntimeDir(),
		Env:          env,
	}
	if err := a.Launcher.EnsureRuntimeFiles(); err != nil {
		k.Close()
		return nil, err
	}
	a.Sys = agents.New(k, st, a.Launcher)
	a.Sys.Repos.Env = env
	if !st.Existed() {
		a.chooseDefaultAgent()
	}
	return a, nil
}

// binary resolves an agent CLI: the configured path, else PATH.
func (a *App) binary(agent string) (string, error) {
	if p := a.Settings.Get().Binaries[agent]; p != "" {
		return p, nil
	}
	return agentcli.LookPath(agent, a.Launcher.Env)
}

// chooseDefaultAgent points every role at the first CLI that is installed, so
// a fresh install works without visiting settings.
func (a *App) chooseDefaultAgent() {
	for _, d := range a.Detect(context.Background()) {
		if !d.Available {
			continue
		}
		roles := map[string]settings.RoleConfig{}
		for name, rc := range a.Settings.Get().Roles {
			rc.Agent = d.Kind
			roles[name] = rc
		}
		if _, err := a.Settings.SetRoles(roles); err != nil {
			log.Printf("default agent: %v", err)
		}
		return
	}
}

// Detect reports which CLIs can run, cached briefly: every status poll asks.
func (a *App) Detect(ctx context.Context) []agentcli.Detection {
	a.detectMu.Lock()
	defer a.detectMu.Unlock()
	if a.detected != nil && time.Since(a.detectedAt) < 20*time.Second {
		return a.detected
	}
	// The answer is shared by every caller for a while: a request that goes
	// away (a page reload) must not kill the probes and cache "not found".
	ctx = context.WithoutCancel(ctx)
	bins := a.Settings.Get().Binaries
	out := make([]agentcli.Detection, len(settings.Agents))
	var wg sync.WaitGroup
	for i, kind := range settings.Agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = agentcli.Detect(ctx, kind, bins[kind], a.Launcher.Env)
		}()
	}
	wg.Wait()
	a.detected, a.detectedAt = out, time.Now()
	return out
}

func (a *App) forgetDetection() {
	a.detectMu.Lock()
	a.detected = nil
	a.catalogs = nil
	a.detectMu.Unlock()
}

// Catalog is what an agent CLI can run, asked of the CLI at most every few
// minutes (codex and pi answer in about a second; a picker opens often).
func (a *App) Catalog(ctx context.Context, agent string) agentcli.Catalog {
	a.detectMu.Lock()
	if c, ok := a.catalogs[agent]; ok && time.Since(c.at) < 5*time.Minute {
		a.detectMu.Unlock()
		return c.catalog
	}
	a.detectMu.Unlock()
	// Cached for every caller, like Detect: one caller's cancellation is not the answer.
	ctx = context.WithoutCancel(ctx)
	c := agentcli.Catalog{Agent: agent}
	if bin, err := a.Launcher.Binary(agent); err != nil {
		c = agentcli.ListModels(ctx, agent, "", a.Launcher.Env)
		c.Error = err.Error()
	} else {
		c = agentcli.ListModels(ctx, agent, bin, a.Launcher.Env)
	}
	a.detectMu.Lock()
	if a.catalogs == nil {
		a.catalogs = map[string]cachedCatalog{}
	}
	a.catalogs[agent] = cachedCatalog{catalog: c, at: time.Now()}
	a.detectMu.Unlock()
	return c
}

type cachedCatalog struct {
	catalog agentcli.Catalog
	at      time.Time
}

// ErrLocked means another runtime already serves this home.
var ErrLocked = fmt.Errorf("another hidane runtime holds the lock on this data directory")

// Start takes the single-runtime lock, reports executions lost to the last
// restart, and starts the agent loop and connectors.
func (a *App) Start() (int, error) {
	release, err := kernel.AcquireRuntimeLock(a.Cfg.Home)
	if err != nil {
		return 0, err
	}
	if release == nil {
		return 0, ErrLocked
	}
	a.release = release
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	lost, err := a.Sys.Pool.Recover(ctx)
	if err != nil {
		cancel()
		release()
		return 0, err
	}
	// A repo moved or deleted while hidane was not running is noticed now,
	// not when the next task trips over it.
	if _, err := a.Sys.Repos.CheckAll(ctx, "kernel:repos"); err != nil {
		log.Printf("checking repositories: %v", err)
	}
	if _, err := a.K.ResumeInterruptedSetups(ctx); err != nil {
		log.Printf("resuming interrupted setups: %v", err)
	}
	a.Runtime = a.Sys.NewRuntime()
	a.Runtime.Start()
	a.goBackground(func() { connectors.Heartbeat(ctx, a.K, a.Cfg.HeartbeatInterval) })
	a.goBackground(func() { connectors.TriageLoop(ctx, a.K, 5*time.Second) })
	a.goBackground(func() { connectors.Scheduler(ctx, a.K, 5*time.Second) })
	a.startFeishu(ctx)
	return lost, nil
}

func (a *App) goBackground(f func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		f()
	}()
}

// Stop ends background work and releases the lock.
func (a *App) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
	a.Sys.Pool.Shutdown(15 * time.Second)
	if a.Runtime != nil {
		a.Runtime.Stop()
	}
	a.wg.Wait()
	if a.release != nil {
		a.release()
		a.release = nil
	}
}

// Close releases storage.
func (a *App) Close() error { return a.K.Close() }

// HandlerOptions select how the API is exposed.
type HandlerOptions struct {
	Desktop     bool
	Token       string
	OnUIReady   func(transport string)
	OnLiveHello func()
	Assets      fs.FS
	// Host exposes the OS to the page (desktop only).
	Host api.Host
}

// Handler is the single http.Handler for the webview and for serve mode.
func (a *App) Handler(o HandlerOptions) http.Handler {
	assets := o.Assets
	if assets == nil {
		assets = frontend.Dist()
	}
	return api.New(api.Options{
		K: a.K, Sys: a.Sys, Settings: a.Settings, Assets: assets,
		Detect: func(ctx context.Context) []agentcli.Detection {
			return a.Detect(ctx)
		},
		Catalog: a.Catalog,
		Desktop: o.Desktop, Token: o.Token, WebhookSecret: a.Cfg.WebhookSecret, Version: Version,
		OnUIReady: o.OnUIReady, OnLiveHello: o.OnLiveHello, Host: o.Host,
		FireSchedule: func(ctx context.Context, sc kernel.Schedule) (string, error) {
			return connectors.FireSchedule(ctx, a.K, sc)
		},
		OnSettingsChanged: a.forgetDetection,
	})
}
