package repos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtsang4/hidane/internal/kernel"
)

// Config is what a repository asks of a fresh worktree.
type Config struct {
	Setup    []string `json:"setup"`
	Teardown []string `json:"teardown"`
	// Source names the files the scripts came from.
	Source []string `json:"source"`
}

// ConfigFile is the person's own config for a repo, kept out of the repo.
func (s *Service) ConfigFile(r kernel.Repo) string {
	return filepath.Join(s.K.Cfg.Home, "repos", r.ID+".json")
}

// Config reads, field by field and first found wins: the person's own file in
// the data directory, the repo's hidane.json, then a paseo.json the repo
// already has. The repo's files are read from the checkout, so they are the
// committed version of the branch it started from.
func (s *Service) Config(r kernel.Repo, dir string) Config {
	var out Config
	for _, path := range []string{s.ConfigFile(r), filepath.Join(dir, "hidane.json"), filepath.Join(dir, "paseo.json")} {
		setup, teardown, ok := readScripts(path)
		if !ok {
			continue
		}
		used := false
		if out.Setup == nil && setup != nil {
			out.Setup, used = setup, true
		}
		if out.Teardown == nil && teardown != nil {
			out.Teardown, used = teardown, true
		}
		if used {
			out.Source = append(out.Source, path)
		}
	}
	return out
}

func readScripts(path string) (setup, teardown []string, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var doc struct {
		Worktree struct {
			Setup    json.RawMessage `json:"setup"`
			Teardown json.RawMessage `json:"teardown"`
		} `json:"worktree"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil, nil, false
	}
	return commands(doc.Worktree.Setup), commands(doc.Worktree.Teardown), true
}

// commands accepts one shell script (run as a whole, stopping at the first
// failing line) or a list of commands run in order.
func commands(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if strings.TrimSpace(one) == "" {
			return []string{}
		}
		return []string{strings.TrimSpace(one)}
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		var out []string
		for _, l := range list {
			if strings.TrimSpace(l) != "" {
				out = append(out, strings.TrimSpace(l))
			}
		}
		return nonNil(out)
	}
	return nil
}

// nonNil: a file that says "no setup" stops the search as much as one that names a script.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
