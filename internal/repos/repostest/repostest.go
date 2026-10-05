// Package repostest makes throwaway git repositories for tests.
package repostest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Hermetic keeps git away from the developer's own configuration and gives
// commits a fixed author for the rest of the test. It uses t.Setenv, which
// rules out t.Parallel.
func Hermetic(t testing.TB) {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "GIT_CONFIG_GLOBAL": filepath.Join(home, "gitconfig"), "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
}

// Git runs git in dir and returns its trimmed output; a failure fails the test.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// New makes a repository called name in a fresh temp directory, with a README
// and files committed on main, and returns its path with symlinks resolved.
func New(t testing.TB, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for p, c := range files {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-qm", "init")
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}
