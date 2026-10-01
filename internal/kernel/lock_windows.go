//go:build windows

package kernel

import (
	"os"
	"path/filepath"
)

// AcquireRuntimeLock uses exclusive creation on Windows; a stale lock file left
// by a crash must be removed by hand.
func AcquireRuntimeLock(home string) (func(), error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "runtime.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return func() {
		f.Close()
		_ = os.Remove(path)
	}, nil
}
