package app_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
