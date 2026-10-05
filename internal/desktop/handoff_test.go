//go:build !nogui

package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/jtsang4/hidane/internal/config"
)

type handedOff struct{}

// A second launch hands off to the running app inside application.New, which
// then exits the process. If the second launch named another HIDANE_HOME,
// nothing may have been created there by then (acceptance saw hidane.db and
// settings.json appear). The stand-in for application.New stops Run at the
// hand-off instead of exiting the test binary.
func TestASecondLaunchCreatesNothingInItsHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "second-home")
	t.Setenv("HIDANE_HOME", home)
	t.Setenv("HIDANE_LOGIN_SHELL", "0")
	// No agent CLI on PATH: a fresh home probes for one to pick a default.
	t.Setenv("PATH", t.TempDir())
	newShell = func(application.Options) *application.App { panic(handedOff{}) }
	t.Cleanup(func() { newShell = application.New })

	func() {
		defer func() {
			if _, ok := recover().(handedOff); !ok {
				t.Fatal("Run never reached the single-instance hand-off")
			}
		}()
		_ = Run(config.Load())
	}()

	entries, err := os.ReadDir(home)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var created []string
	for _, e := range entries {
		created = append(created, e.Name())
	}
	if len(created) > 0 {
		t.Fatalf("created in the second home before handing off: %v", created)
	}
}
