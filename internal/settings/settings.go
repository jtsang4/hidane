// Package settings is the desktop app's model configuration: which local agent
// CLI each role runs on, and the LLM providers those CLIs may be pointed at.
// It lives in HIDANE_HOME/settings.json (0600); the API never returns a key.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Agent CLIs hidane can drive.
const (
	Claude = "claude"
	Codex  = "codex"
	Pi     = "pi"
)

var Agents = []string{Claude, Codex, Pi}

// Roles: one loop at three scopes, plus the memory distiller.
var Roles = []string{"primary", "manager", "worker", "distiller"}

// EffortsFor are the reasoning efforts an agent CLI accepts ("" = its default):
// claude --effort, codex model_reasoning_effort (the union over its models;
// the catalog says which a model takes), pi --thinking.
func EffortsFor(agent string) []string {
	switch agent {
	case Claude:
		return []string{"", "low", "medium", "high", "xhigh", "max"}
	case Codex:
		return []string{"", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}
	case Pi:
		return []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"}
	}
	return []string{""}
}

// Provider is an LLM endpoint the CLIs can be pointed at. Each CLI speaks one
// wire protocol, so a provider says which of them it can serve:
// AnthropicBaseURL for Claude Code, OpenAIBaseURL (Responses API) for Codex,
// PiProvider (a provider name in pi's catalog) for pi.
type Provider struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	AnthropicBaseURL string   `json:"anthropicBaseUrl"`
	OpenAIBaseURL    string   `json:"openaiBaseUrl"`
	PiProvider       string   `json:"piProvider"`
	APIKey           string   `json:"apiKey"`
	Models           []string `json:"models"`
}

// RoleConfig selects how one role runs. Provider "" means the CLI's own login
// and defaults: nothing is injected.
type RoleConfig struct {
	Agent    string `json:"agent"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

type Settings struct {
	Providers []Provider            `json:"providers"`
	Roles     map[string]RoleConfig `json:"roles"`
	// Binaries are absolute-path overrides; empty means look it up on PATH.
	Binaries map[string]string `json:"binaries"`
	// Feishu enables the Feishu channel (long connection; no public URL needed).
	Feishu *Feishu `json:"feishu,omitempty"`
}

// Feishu is a self-built app's credentials.
type Feishu struct {
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	// AllowedUsers are the open_ids whose messages reach the agents.
	AllowedUsers []string `json:"allowedUsers,omitempty"`
}

func Default() Settings {
	return Settings{
		Providers: []Provider{},
		Roles: map[string]RoleConfig{
			"primary":   {Agent: Claude, Effort: "low"},
			"manager":   {Agent: Claude, Effort: "low"},
			"worker":    {Agent: Claude, Effort: "medium"},
			"distiller": {Agent: Claude, Effort: "low"},
		},
		Binaries: map[string]string{Claude: "", Codex: "", Pi: ""},
	}
}

func (s Settings) clone() Settings {
	b, _ := json.Marshal(s)
	var out Settings
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *Settings) normalize() {
	d := Default()
	if s.Providers == nil {
		s.Providers = []Provider{}
	}
	if s.Roles == nil {
		s.Roles = map[string]RoleConfig{}
	}
	for _, r := range Roles {
		if _, ok := s.Roles[r]; !ok {
			s.Roles[r] = d.Roles[r]
		}
	}
	if s.Binaries == nil {
		s.Binaries = map[string]string{}
	}
	for _, a := range Agents {
		if _, ok := s.Binaries[a]; !ok {
			s.Binaries[a] = ""
		}
	}
	for i := range s.Providers {
		if s.Providers[i].Models == nil {
			s.Providers[i].Models = []string{}
		}
	}
}

// Store guards the file. Changes are written atomically.
type Store struct {
	path    string
	mu      sync.Mutex
	s       Settings
	existed bool
}

// Load reads the file, or starts from defaults when there is none.
func Load(path string) (*Store, error) {
	st := &Store{path: path, s: Default()}
	b, err := os.ReadFile(path)
	if err == nil {
		var s Settings
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("settings %s: %w", path, err)
		}
		s.normalize()
		st.s = s
		st.existed = true
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return st, nil
}

func (st *Store) Path() string { return st.path }

// Existed reports whether a settings file was present at load.
func (st *Store) Existed() bool { return st.existed }

// Get returns a copy, keys included (for launching agents, never for the API).
func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.clone()
}

func (st *Store) save(s Settings) error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, st.path); err != nil {
		return err
	}
	st.s = s
	st.existed = true
	return nil
}

// Update applies fn to a copy, validates the result and saves it.
func (st *Store) Update(fn func(*Settings) error) (Settings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	next := st.s.clone()
	if err := fn(&next); err != nil {
		return st.s.clone(), err
	}
	next.normalize()
	if err := Validate(next); err != nil {
		return st.s.clone(), err
	}
	if err := st.save(next); err != nil {
		return st.s.clone(), err
	}
	return next.clone(), nil
}

// Error is a validation failure to show to the person.
type Error struct{ Msg string }

func (e Error) Error() string { return e.Msg }

func errf(format string, args ...any) error { return Error{fmt.Sprintf(format, args...)} }

func (s Settings) Provider(id string) (Provider, bool) {
	for _, p := range s.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Compatibility says why an agent cannot use a provider, or "".
func Compatibility(agent string, p *Provider) string {
	if p == nil {
		return ""
	}
	switch agent {
	case Claude:
		if p.AnthropicBaseURL == "" {
			return fmt.Sprintf("provider %q has no Anthropic-compatible base URL, which Claude Code needs", p.Label)
		}
	case Codex:
		if p.OpenAIBaseURL == "" {
			return fmt.Sprintf("provider %q has no OpenAI Responses base URL, which Codex needs", p.Label)
		}
	case Pi:
		if p.PiProvider == "" {
			return fmt.Sprintf("provider %q has no pi provider name, which pi needs", p.Label)
		}
	}
	return ""
}

var (
	idPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	urlPattern = regexp.MustCompile(`^https?://\S+$`)
	piPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Validate checks the whole document.
func Validate(s Settings) error {
	seen := map[string]bool{}
	for _, p := range s.Providers {
		if !idPattern.MatchString(p.ID) {
			return errf("provider id %q must be lowercase letters, digits and dashes", p.ID)
		}
		if seen[p.ID] {
			return errf("duplicate provider id %q", p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Label) == "" {
			return errf("provider %q needs a label", p.ID)
		}
		for _, u := range []string{p.AnthropicBaseURL, p.OpenAIBaseURL} {
			if u != "" && !urlPattern.MatchString(u) {
				return errf("provider %q: %q is not an http(s) URL", p.ID, u)
			}
		}
		if p.PiProvider != "" && !piPattern.MatchString(p.PiProvider) {
			return errf("provider %q: pi provider name %q is invalid", p.ID, p.PiProvider)
		}
		if p.AnthropicBaseURL == "" && p.OpenAIBaseURL == "" && p.PiProvider == "" {
			return errf("provider %q needs at least one of: Anthropic base URL, OpenAI base URL, pi provider", p.ID)
		}
	}
	for _, role := range Roles {
		rc, ok := s.Roles[role]
		if !ok {
			return errf("role %q is not configured", role)
		}
		if err := s.ValidateRun(rc); err != nil {
			return errf("role %s: %v", role, err)
		}
	}
	for kind, path := range s.Binaries {
		if !contains(Agents, kind) {
			return errf("unknown agent %q", kind)
		}
		if path != "" && !filepath.IsAbs(path) {
			return errf("%s path must be absolute", kind)
		}
	}
	return nil
}

// ValidateRun checks one way of running an agent: a role's, or a work
// item's own choice.
func (s Settings) ValidateRun(rc RoleConfig) error {
	if !contains(Agents, rc.Agent) {
		return fmt.Errorf("agent must be one of %s", strings.Join(Agents, ", "))
	}
	if efforts := EffortsFor(rc.Agent); !contains(efforts, rc.Effort) {
		return fmt.Errorf("%s effort must be one of %s", rc.Agent, strings.Join(efforts[1:], ", "))
	}
	if rc.Provider != "" {
		p, ok := s.Provider(rc.Provider)
		if !ok {
			return fmt.Errorf("unknown provider %q", rc.Provider)
		}
		if msg := Compatibility(rc.Agent, &p); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	if strings.ContainsAny(rc.Model, " \n\t") {
		return fmt.Errorf("model must not contain spaces")
	}
	return nil
}

// Resolved is a role's effective configuration.
type Resolved struct {
	Role     string
	Agent    string
	Model    string
	Effort   string
	Provider *Provider
}

// Resolve returns how a role should run now.
func (s Settings) Resolve(role string) Resolved { return s.ResolveWith(role, nil) }

// ResolveWith is Resolve with a work item's own choice in place of the
// role's configuration (nil: the role's).
func (s Settings) ResolveWith(role string, own *RoleConfig) Resolved {
	rc, ok := s.Roles[role]
	if !ok {
		rc = Default().Roles[role]
	}
	if own != nil && own.Agent != "" {
		rc = *own
	}
	r := Resolved{Role: role, Agent: rc.Agent, Model: rc.Model, Effort: rc.Effort}
	if rc.Provider != "" {
		if p, ok := s.Provider(rc.Provider); ok {
			r.Provider = &p
		}
	}
	return r
}

// Describe is a one-line summary (no secrets) for status displays.
func (r Resolved) Describe() string {
	via := "own login"
	if r.Provider != nil {
		via = r.Provider.Label
	}
	model := r.Model
	if model == "" {
		model = "default model"
	}
	return fmt.Sprintf("%s (%s, %s)", r.Agent, via, model)
}

// ProviderView is a provider as the API shows it: the key never leaves.
type ProviderView struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	AnthropicBaseURL string   `json:"anthropicBaseUrl"`
	OpenAIBaseURL    string   `json:"openaiBaseUrl"`
	PiProvider       string   `json:"piProvider"`
	Models           []string `json:"models"`
	HasAPIKey        bool     `json:"hasApiKey"`
	APIKeyHint       string   `json:"apiKeyHint"`
}

func KeyHint(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "…"
	}
	return "…" + key[len(key)-4:]
}

func ViewOf(p Provider) ProviderView {
	models := p.Models
	if models == nil {
		models = []string{}
	}
	return ProviderView{ID: p.ID, Label: p.Label, AnthropicBaseURL: p.AnthropicBaseURL, OpenAIBaseURL: p.OpenAIBaseURL,
		PiProvider: p.PiProvider, Models: models, HasAPIKey: p.APIKey != "", APIKeyHint: KeyHint(p.APIKey)}
}

type View struct {
	Path      string                `json:"path"`
	Providers []ProviderView        `json:"providers"`
	Roles     map[string]RoleConfig `json:"roles"`
	Binaries  map[string]string     `json:"binaries"`
}

func (st *Store) View() View {
	s := st.Get()
	v := View{Path: st.path, Providers: []ProviderView{}, Roles: s.Roles, Binaries: s.Binaries}
	for _, p := range s.Providers {
		v.Providers = append(v.Providers, ViewOf(p))
	}
	return v
}

// ProviderInput creates or patches a provider; nil fields are left unchanged.
// APIKey nil keeps the stored key, "" clears it.
type ProviderInput struct {
	ID               *string   `json:"id"`
	Label            *string   `json:"label"`
	AnthropicBaseURL *string   `json:"anthropicBaseUrl"`
	OpenAIBaseURL    *string   `json:"openaiBaseUrl"`
	PiProvider       *string   `json:"piProvider"`
	APIKey           *string   `json:"apiKey"`
	Models           *[]string `json:"models"`
}

func trimURL(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }

func cleanModels(xs []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (in ProviderInput) apply(p *Provider) {
	if in.Label != nil {
		p.Label = strings.TrimSpace(*in.Label)
	}
	if in.AnthropicBaseURL != nil {
		p.AnthropicBaseURL = trimURL(*in.AnthropicBaseURL)
	}
	if in.OpenAIBaseURL != nil {
		p.OpenAIBaseURL = trimURL(*in.OpenAIBaseURL)
	}
	if in.PiProvider != nil {
		p.PiProvider = strings.TrimSpace(*in.PiProvider)
	}
	if in.APIKey != nil {
		p.APIKey = strings.TrimSpace(*in.APIKey)
	}
	if in.Models != nil {
		p.Models = cleanModels(*in.Models)
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(label string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if s == "" || !(s[0] >= 'a' && s[0] <= 'z') {
		s = "p-" + s
	}
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	return s
}

func (st *Store) AddProvider(in ProviderInput) (Provider, error) {
	var created Provider
	_, err := st.Update(func(s *Settings) error {
		p := Provider{Models: []string{}}
		in.apply(&p)
		if in.ID != nil && strings.TrimSpace(*in.ID) != "" {
			p.ID = strings.TrimSpace(*in.ID)
		} else {
			base := slug(p.Label)
			p.ID = base
			for i := 2; ; i++ {
				if _, taken := s.Provider(p.ID); !taken {
					break
				}
				p.ID = fmt.Sprintf("%s-%d", base, i)
			}
		}
		if _, taken := s.Provider(p.ID); taken {
			return errf("provider id %q already exists", p.ID)
		}
		s.Providers = append(s.Providers, p)
		created = p
		return nil
	})
	return created, err
}

// ErrNotFound marks an unknown provider.
var ErrNotFound = fmt.Errorf("not found")

func (st *Store) UpdateProvider(id string, in ProviderInput) (Provider, error) {
	var updated Provider
	_, err := st.Update(func(s *Settings) error {
		for i := range s.Providers {
			if s.Providers[i].ID == id {
				in.apply(&s.Providers[i])
				updated = s.Providers[i]
				return nil
			}
		}
		return ErrNotFound
	})
	return updated, err
}

// RolesUsing lists roles that reference a provider.
func (s Settings) RolesUsing(id string) []string {
	var out []string
	for _, r := range Roles {
		if s.Roles[r].Provider == id {
			out = append(out, r)
		}
	}
	return out
}

// InUseError refuses deleting a provider a role still runs on.
type InUseError struct{ Roles []string }

func (e InUseError) Error() string {
	return "provider is used by: " + strings.Join(e.Roles, ", ")
}

func (st *Store) DeleteProvider(id string) error {
	_, err := st.Update(func(s *Settings) error {
		if roles := s.RolesUsing(id); len(roles) > 0 {
			return InUseError{roles}
		}
		kept := s.Providers[:0:0]
		found := false
		for _, p := range s.Providers {
			if p.ID == id {
				found = true
				continue
			}
			kept = append(kept, p)
		}
		if !found {
			return ErrNotFound
		}
		s.Providers = kept
		return nil
	})
	return err
}

func (st *Store) SetRoles(roles map[string]RoleConfig) (Settings, error) {
	return st.Update(func(s *Settings) error {
		for r, rc := range roles {
			if !contains(Roles, r) {
				return errf("unknown role %q", r)
			}
			rc.Model = strings.TrimSpace(rc.Model)
			s.Roles[r] = rc
		}
		return nil
	})
}

func (st *Store) SetBinaries(b map[string]string) (Settings, error) {
	return st.Update(func(s *Settings) error {
		for k, v := range b {
			if !contains(Agents, k) {
				return errf("unknown agent %q", k)
			}
			s.Binaries[k] = strings.TrimSpace(v)
		}
		return nil
	})
}

// Preset pre-fills the provider form; once saved it is an ordinary provider.
type Preset struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	AnthropicBaseURL string   `json:"anthropicBaseUrl"`
	OpenAIBaseURL    string   `json:"openaiBaseUrl"`
	PiProvider       string   `json:"piProvider"`
	Models           []string `json:"models"`
	DocsURL          string   `json:"docsUrl"`
}

func Presets() []Preset {
	ps := []Preset{
		{ID: "anthropic", Label: "Anthropic", AnthropicBaseURL: "https://api.anthropic.com", PiProvider: "anthropic",
			Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-4-5-20251001"}, DocsURL: "https://docs.anthropic.com"},
		{ID: "openai", Label: "OpenAI", OpenAIBaseURL: "https://api.openai.com/v1", PiProvider: "openai",
			Models: []string{}, DocsURL: "https://platform.openai.com/docs"},
		{ID: "deepseek", Label: "DeepSeek", AnthropicBaseURL: "https://api.deepseek.com/anthropic", PiProvider: "deepseek",
			Models: []string{"deepseek-v4-pro", "deepseek-v4-flash-vision-exp", "deepseek-flash"}, DocsURL: "https://api-docs.deepseek.com"},
		{ID: "moonshot", Label: "Moonshot (Kimi)", AnthropicBaseURL: "https://api.moonshot.cn/anthropic", PiProvider: "moonshotai-cn",
			Models: []string{}, DocsURL: "https://platform.moonshot.cn/docs"},
		{ID: "kimi-coding", Label: "Kimi For Coding", AnthropicBaseURL: "https://api.kimi.com/coding", PiProvider: "kimi-coding",
			Models: []string{"k3"}, DocsURL: "https://www.kimi.com/coding/docs"},
		{ID: "zhipu", Label: "Zhipu GLM (China)", AnthropicBaseURL: "https://open.bigmodel.cn/api/anthropic", PiProvider: "zai-coding-cn",
			Models: []string{}, DocsURL: "https://docs.bigmodel.cn"},
		{ID: "zai", Label: "Z.AI (GLM, global)", AnthropicBaseURL: "https://api.z.ai/api/anthropic", PiProvider: "zai",
			Models: []string{}, DocsURL: "https://docs.z.ai"},
		{ID: "openrouter", Label: "OpenRouter", AnthropicBaseURL: "https://openrouter.ai/api", OpenAIBaseURL: "https://openrouter.ai/api/v1", PiProvider: "openrouter",
			Models: []string{}, DocsURL: "https://openrouter.ai/docs"},
		{ID: "opencode-go", Label: "OpenCode Go", PiProvider: "opencode-go",
			Models: []string{"deepseek-v4-flash-vision-exp", "kimi-k3", "qwen3.8-max", "glm-5.3"}, DocsURL: "https://opencode.ai"},
	}
	return ps
}
