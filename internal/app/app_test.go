package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/agentcli/fakecli"
	"github.com/jtsang4/hidane/internal/app"
	"github.com/jtsang4/hidane/internal/config"
)

// onlyCodex exposes just the fake codex on PATH.
func onlyCodex(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(fakecli.Dir(t), "fakeagent"), filepath.Join(dir, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
}

func TestFirstRunPicksAnInstalledCLIAndHoldsTheLock(t *testing.T) {
	onlyCodex(t)
	cfg := config.ForTest(t.TempDir())
	a, err := app.Open(cfg, app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for role, rc := range a.Settings.Get().Roles {
		if rc.Agent != "codex" {
			t.Fatalf("%s runs on %s; a fresh install should use the only installed CLI", role, rc.Agent)
		}
	}
	if _, err := a.Start(); err != nil {
		t.Fatal(err)
	}
	second, err := app.Open(cfg, app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Start(); !errors.Is(err, app.ErrLocked) {
		t.Fatalf("a second runtime on one home must be refused: %v", err)
	}
	second.Close()

	srv := httptest.NewServer(a.Handler(app.HandlerOptions{Token: "t"}))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/health")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("health: %v %v", res, err)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/agents", nil)
	req.Header.Set("Authorization", "Bearer t")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Agents []struct {
			Kind      string `json:"kind"`
			Available bool   `json:"available"`
		} `json:"agents"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	avail := map[string]bool{}
	for _, d := range body.Agents {
		avail[d.Kind] = d.Available
	}
	if !avail["codex"] || avail["claude"] || avail["pi"] {
		t.Fatalf("detection: %+v", body.Agents)
	}
	a.Stop()
	third, _ := app.Open(cfg, app.Options{})
	if _, err := third.Start(); err != nil {
		t.Fatalf("the lock is released on stop: %v", err)
	}
	third.Stop()
	third.Close()
}

func TestAnExistingSettingsFileIsLeftAlone(t *testing.T) {
	onlyCodex(t)
	cfg := config.ForTest(t.TempDir())
	_ = os.WriteFile(cfg.SettingsPath(), []byte(`{"roles":{"primary":{"agent":"pi"}}}`), 0o600)
	a, err := app.Open(cfg, app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if got := a.Settings.Get().Roles["primary"].Agent; got != "pi" {
		t.Fatalf("a person's choice is never overwritten: %s", got)
	}
}

// The detection is cached for every caller: a request cancelled mid-probe (a
// reload) must not leave every CLI reported as missing.
func TestACancelledRequestDoesNotCacheMissingCLIs(t *testing.T) {
	onlyCodex(t)
	a, err := app.Open(config.ForTest(t.TempDir()), app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := httptest.NewServer(a.Handler(app.HandlerOptions{Token: "t"}))
	defer srv.Close()
	// Changing a binary path forgets the cached detection.
	req, _ := http.NewRequest("PUT", srv.URL+"/api/settings/binaries", strings.NewReader(`{"binaries":{"pi":""}}`))
	req.Header.Set("Authorization", "Bearer t")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("binaries: %v %v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, when := range []struct {
		name string
		ctx  context.Context
	}{{"the cancelled request", ctx}, {"the next caller", context.Background()}} {
		for _, d := range a.Detect(when.ctx) {
			if d.Kind == "codex" && !d.Available {
				t.Fatalf("%s sees codex as missing: %s", when.name, d.Error)
			}
		}
	}
	if c := a.Catalog(ctx, "codex"); c.Error != "" || len(c.Models) == 0 {
		t.Fatalf("a cancelled request cached an empty codex catalog: %+v", c)
	}
}
