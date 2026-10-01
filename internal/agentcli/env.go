package agentcli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Variables a parent agent session sets that make a child CLI misbehave
// (Claude Code refuses to start "inside another session").
var parentSessionVars = []string{
	"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT", "CLAUDE_AGENT_SDK_VERSION",
	"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED",
}

var (
	loginPathOnce sync.Once
	loginPath     string
)

// LoginShellPath asks the user's login shell for PATH. An app started from
// Finder inherits a minimal PATH that contains none of the places these CLIs
// are installed (~/.local/bin, ~/.bun/bin, mise/asdf shims, Homebrew).
func LoginShellPath() string {
	loginPathOnce.Do(func() {
		if runtime.GOOS == "windows" {
			return
		}
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		const marker = "__HIDANE_PATH__"
		cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "printf '"+marker+"%s"+marker+"' \"$PATH\"")
		cmd.Stdin = nil
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			return
		}
		s := string(out)
		if i := strings.Index(s, marker); i >= 0 {
			s = s[i+len(marker):]
			if j := strings.Index(s, marker); j >= 0 {
				loginPath = s[:j]
			}
		}
	})
	return loginPath
}

func commonBinDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".asdf", "shims"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, "go", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	}
}

// MergePath joins PATH lists, keeping the first occurrence of each directory.
func MergePath(parts ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		for _, d := range filepath.SplitList(p) {
			if d != "" && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// BaseEnv is the environment every CLI starts from: this process's
// environment, a PATH that can find the CLIs, and no parent-session markers.
func BaseEnv(resolveLoginShell bool) []string {
	path := os.Getenv("PATH")
	if resolveLoginShell {
		path = MergePath(path, LoginShellPath())
	}
	path = MergePath(path, strings.Join(commonBinDirs(), string(os.PathListSeparator)))
	env := WithoutVars(os.Environ(), append([]string{"PATH"}, parentSessionVars...)...)
	return append(env, "PATH="+path)
}

// WithoutVars drops variables by name.
func WithoutVars(env []string, names ...string) []string {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if !drop[name] {
			out = append(out, kv)
		}
	}
	return out
}

// WithoutPrefix drops variables whose name starts with any prefix.
func WithoutPrefix(env []string, prefixes ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		keep := true
		for _, p := range prefixes {
			if strings.HasPrefix(kv, p) {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

func envValue(env []string, name string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], name+"=") {
			return env[i][len(name)+1:]
		}
	}
	return ""
}

// LookPath finds an executable on the PATH of env.
func LookPath(name string, env []string) (string, error) {
	for _, dir := range filepath.SplitList(envValue(env, "PATH")) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found on PATH", name)
}

// Detection is whether one CLI can be run.
type Detection struct {
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

// Detect runs `<bin> --version`.
func Detect(ctx context.Context, kind, override string, env []string) Detection {
	d := Detection{Kind: kind}
	path := override
	if path == "" {
		p, err := LookPath(kind, env)
		if err != nil {
			d.Error = err.Error()
			return d
		}
		path = p
	}
	d.Path = path
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = append(env, "PI_OFFLINE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		d.Error = strings.TrimSpace(fmt.Sprintf("%v %s", err, firstLine(string(out))))
		return d
	}
	d.Available = true
	d.Version = firstLine(string(out))
	return d
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
