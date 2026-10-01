//go:build !windows

package kernel

import (
	"os"
	"path/filepath"
	"syscall"
)

// AcquireRuntimeLock holds the single-runtime lock for this home. Two runtimes
// against one database would run the same turn twice. Returns nil, nil when
// another process holds it.
func AcquireRuntimeLock(home string) (func(), error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, "runtime.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, nil
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
