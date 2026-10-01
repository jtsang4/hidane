//go:build !nogui

package desktop

import (
	"os/exec"
	"runtime"
)

// openWithOS opens a URL in the default browser, or reveals a file in the
// file manager.
func openWithOS(target string, reveal bool) error {
	switch runtime.GOOS {
	case "darwin":
		if reveal {
			return exec.Command("open", "-R", target).Start()
		}
		return exec.Command("open", target).Start()
	case "windows":
		if reveal {
			return exec.Command("explorer", "/select,", target).Start()
		}
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
