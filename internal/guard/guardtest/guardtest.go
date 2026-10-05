// Package guardtest builds the real `hidane guard` hook once per test binary.
//
// A test binary must not serve as its own hook command: under -race every
// hook call would re-execute the race-instrumented binary, whose runtime takes
// about a second to start on macOS — one per tool call the fake CLIs make.
package guardtest

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
	bin  string
	err  error
)

// Command returns the path of a headless hidane binary for Launcher.GuardCommand.
func Command(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		var dir string
		dir, err = os.MkdirTemp("", "hidane-guard-")
		if err != nil {
			return
		}
		_, file, _, _ := runtime.Caller(0)
		bin = filepath.Join(dir, "hidane")
		cmd := exec.Command("go", "build", "-tags", "nogui", "-o", bin, ".")
		cmd.Dir = filepath.Join(filepath.Dir(file), "..", "..", "..")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, e := cmd.CombinedOutput(); e != nil {
			err = &buildError{string(out), e}
		}
	})
	if err != nil {
		t.Fatalf("build hidane guard: %v", err)
	}
	return bin
}

type buildError struct {
	out string
	err error
}

func (b *buildError) Error() string { return b.err.Error() + "\n" + b.out }
