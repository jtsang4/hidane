// Package kerneltest builds an isolated kernel (temp home, temp SQLite) for tests.
package kerneltest

import (
	"testing"

	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/kernel"
)

// New returns a kernel rooted in a fresh temp directory, closed at test end.
func New(t testing.TB) *kernel.Kernel {
	t.Helper()
	cfg := config.ForTest(t.TempDir())
	k, err := kernel.Open(cfg)
	if err != nil {
		t.Fatalf("open kernel: %v", err)
	}
	t.Cleanup(func() { k.Close() })
	return k
}
