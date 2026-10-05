package agentcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/jtsang4/hidane/internal/settings"
)

// Model is one model an agent CLI can run, with the efforts it takes.
type Model struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Efforts       []string `json:"efforts"`
	DefaultEffort string   `json:"defaultEffort"`
}

// Catalog is what an agent CLI offers to run on its own login: its models
// and the reasoning efforts it accepts. Source says whether the CLI was
// asked ("cli") or the built-in list stands in ("builtin"); a model not
// listed can still be named.
type Catalog struct {
	Agent   string   `json:"agent"`
	Models  []Model  `json:"models"`
	Efforts []string `json:"efforts"`
	Source  string   `json:"source"`
	Error   string   `json:"error,omitempty"`
}

// Claude Code has no command that lists models; its aliases follow the
// newest model of each family.
var claudeModels = []Model{
	{ID: "opus", Label: "Opus (latest)"},
	{ID: "sonnet", Label: "Sonnet (latest)"},
	{ID: "haiku", Label: "Haiku (latest)"},
	{ID: "claude-fable-5-1", Label: "Fable 5.1"},
	{ID: "claude-opus-5-5", Label: "Opus 5.5"},
	{ID: "claude-sonnet-5-5", Label: "Sonnet 5.5"},
	{ID: "claude-haiku-4-5-20251001", Label: "Haiku 4.5"},
}

// ListModels asks an agent CLI what it can run. path is its executable.
func ListModels(ctx context.Context, kind, path string, env []string) Catalog {
	c := Catalog{Agent: kind, Efforts: settings.EffortsFor(kind)[1:], Source: "builtin", Models: []Model{}}
	switch kind {
	case settings.Claude:
		c.Models = append(c.Models, claudeModels...)
		return c
	case settings.Codex:
		out, err := runQuiet(ctx, path, env, "debug", "models")
		if err != nil {
			c.Error = err.Error()
			return c
		}
		var doc struct {
			Models []struct {
				Slug        string `json:"slug"`
				DisplayName string `json:"display_name"`
				Visibility  string `json:"visibility"`
				Default     string `json:"default_reasoning_level"`
				Levels      []struct {
					Effort string `json:"effort"`
				} `json:"supported_reasoning_levels"`
			} `json:"models"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			c.Error = "codex debug models: " + err.Error()
			return c
		}
		for _, m := range doc.Models {
			if m.Slug == "" || (m.Visibility != "" && m.Visibility != "list") {
				continue
			}
			model := Model{ID: m.Slug, Label: m.DisplayName, DefaultEffort: m.Default}
			for _, l := range m.Levels {
				model.Efforts = append(model.Efforts, l.Effort)
			}
			c.Models = append(c.Models, model)
		}
		c.Source = "cli"
	case settings.Pi:
		out, err := runQuiet(ctx, path, env, "--list-models")
		if err != nil {
			c.Error = err.Error()
			return c
		}
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 2 || f[0] == "provider" {
				continue
			}
			c.Models = append(c.Models, Model{ID: f[0] + "/" + f[1], Label: f[1] + " (" + f[0] + ")"})
		}
		c.Source = "cli"
	}
	return c
}

func runQuiet(ctx context.Context, path string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(WithoutVars(env, parentSessionVars...), "PI_OFFLINE=1")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, errors.New(firstLine(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}
