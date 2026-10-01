package settings_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/settings"
)

func TestDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := settings.Load(path)
	if err != nil || st.Existed() {
		t.Fatal("no file yet")
	}
	r := st.Get().Resolve("worker")
	if r.Agent != "claude" || r.Provider != nil || r.Effort != "medium" {
		t.Fatalf("defaults: %+v", r)
	}
	key := "sk-abcdef123456"
	p, err := st.AddProvider(settings.ProviderInput{Label: ptr("Moonshot (Kimi)"), PiProvider: ptr("moonshotai-cn"), APIKey: &key})
	if err != nil || p.ID != "moonshot-kimi" {
		t.Fatalf("slug id: %+v %v", p, err)
	}
	p2, _ := st.AddProvider(settings.ProviderInput{Label: ptr("Moonshot (Kimi)"), PiProvider: ptr("moonshotai")})
	if p2.ID != "moonshot-kimi-2" {
		t.Fatalf("unique ids: %s", p2.ID)
	}
	if _, err := st.SetRoles(map[string]settings.RoleConfig{"distiller": {Agent: "pi", Provider: p.ID, Model: "kimi-k3", Effort: "low"}}); err != nil {
		t.Fatal(err)
	}
	again, err := settings.Load(path)
	if err != nil || !again.Existed() {
		t.Fatal(err)
	}
	res := again.Get().Resolve("distiller")
	if res.Provider == nil || res.Provider.APIKey != key || res.Model != "kimi-k3" {
		t.Fatalf("reloaded: %+v", res)
	}
	if !strings.Contains(res.Describe(), "Moonshot (Kimi)") || strings.Contains(res.Describe(), key) {
		t.Fatal("describe names the provider, never the key")
	}
	v := again.View()
	if v.Providers[0].APIKeyHint != "…3456" || !v.Providers[0].HasAPIKey {
		t.Fatalf("view: %+v", v.Providers[0])
	}
	b, _ := os.ReadFile(path)
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 || !strings.Contains(string(b), key) {
		t.Fatal("the file keeps the key, privately")
	}
}

func ptr(s string) *string { return &s }

func TestValidation(t *testing.T) {
	st, _ := settings.Load(filepath.Join(t.TempDir(), "s.json"))
	cases := []settings.ProviderInput{
		{Label: ptr("No endpoints")},
		{Label: ptr("Bad URL"), AnthropicBaseURL: ptr("ftp://x")},
		{ID: ptr("Bad ID"), Label: ptr("x"), PiProvider: ptr("deepseek")},
		{Label: ptr(" "), PiProvider: ptr("deepseek")},
	}
	for _, c := range cases {
		if _, err := st.AddProvider(c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	if _, err := st.SetRoles(map[string]settings.RoleConfig{"primary": {Agent: "gpt"}}); err == nil {
		t.Error("unknown agent")
	}
	if _, err := st.SetRoles(map[string]settings.RoleConfig{"primary": {Agent: "claude", Provider: "ghost"}}); err == nil {
		t.Error("unknown provider")
	}
	if _, err := st.SetRoles(map[string]settings.RoleConfig{"primary": {Agent: "claude", Effort: "max"}}); err == nil {
		t.Error("unknown effort")
	}
	if _, err := st.SetRoles(map[string]settings.RoleConfig{"janitor": {Agent: "claude"}}); err == nil {
		t.Error("unknown role")
	}
	p, _ := st.AddProvider(settings.ProviderInput{Label: ptr("OR"), OpenAIBaseURL: ptr("https://openrouter.ai/api/v1/")})
	if p.OpenAIBaseURL != "https://openrouter.ai/api/v1" {
		t.Fatal("trailing slash trimmed")
	}
	for agent, ok := range map[string]bool{"codex": true, "claude": false, "pi": false} {
		_, err := st.SetRoles(map[string]settings.RoleConfig{"worker": {Agent: agent, Provider: p.ID}})
		if (err == nil) != ok {
			t.Errorf("%s with an OpenAI-only provider: %v", agent, err)
		}
	}
	if err := st.DeleteProvider(p.ID); err == nil {
		t.Fatal("a provider in use cannot be deleted")
	}
	if _, err := st.SetRoles(map[string]settings.RoleConfig{"worker": {Agent: "claude"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProvider(p.ID); err == nil {
		t.Fatal("unknown provider")
	}
	for _, pre := range settings.Presets() {
		if pre.AnthropicBaseURL == "" && pre.OpenAIBaseURL == "" && pre.PiProvider == "" {
			t.Errorf("preset %s serves no CLI", pre.ID)
		}
	}
}
