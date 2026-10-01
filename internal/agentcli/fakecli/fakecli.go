// Package fakecli builds cmd/fakeagent once per test binary and exposes it as
// claude, codex and pi.
package fakecli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	once sync.Once
	dir  string
	err  error
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Dir returns a directory holding claude, codex and pi fakes.
func Dir(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		dir, err = os.MkdirTemp("", "hidane-fakeagent-")
		if err != nil {
			return
		}
		bin := filepath.Join(dir, "fakeagent")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/fakeagent")
		cmd.Dir = repoRoot()
		if out, e := cmd.CombinedOutput(); e != nil {
			err = &buildError{string(out), e}
			return
		}
		for _, name := range []string{"claude", "codex", "pi"} {
			if e := os.Symlink(bin, filepath.Join(dir, name)); e != nil {
				err = e
				return
			}
		}
	})
	if err != nil {
		t.Fatalf("build fakeagent: %v", err)
	}
	return dir
}

type buildError struct {
	out string
	err error
}

func (b *buildError) Error() string { return b.err.Error() + "\n" + b.out }
