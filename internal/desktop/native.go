//go:build !nogui

package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/jtsang4/hidane/internal/kernel"
)

// CommandEvent carries a menu command to the page; the page owns navigation.
const CommandEvent = "hidane:command"

// host is the operating system as the page sees it (api.Host).
type host struct {
	app           *application.App
	dock          *dock.DockService
	notifications *notifications.NotificationService
	asked         bool
}

func (h *host) Open(target string, reveal bool) error {
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
		if reveal {
			target = filepath.Dir(target)
		}
		return exec.Command("xdg-open", target).Start()
	}
}

func (h *host) CopyText(text string) error {
	if !h.app.Clipboard.SetText(text) {
		return fmt.Errorf("the clipboard refused the text")
	}
	return nil
}

// inBundle: macOS notifications need the app's bundle identifier, and the
// service refuses to start — taking the whole app down — without one.
func inBundle() bool {
	if runtime.GOOS != "darwin" {
		return true
	}
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

// Notify asks for permission on first use; the OS remembers the answer.
func (h *host) Notify(title, body string) error {
	if h.notifications == nil {
		return fmt.Errorf("notifications need the packaged app (make app)")
	}
	if !h.asked {
		h.asked = true
		if ok, err := h.notifications.CheckNotificationAuthorization(); err == nil && !ok {
			if _, err := h.notifications.RequestNotificationAuthorization(); err != nil {
				return err
			}
		}
	}
	return h.notifications.SendNotification(notifications.NotificationOptions{
		ID: kernel.GenID("ntf", 8), Title: title, Body: body,
	})
}

func (h *host) SetBadge(count int) error {
	if count <= 0 {
		return h.dock.RemoveBadge()
	}
	label := strconv.Itoa(count)
	if count > 99 {
		label = "99+"
	}
	return h.dock.SetBadge(label)
}

// menu is the native application menu. Commands are handed to the page as
// events; accelerators therefore work even while focus is in a text field.
func menu(app *application.App, send func(command string)) *application.Menu {
	m := application.NewMenu()
	item := func(parent *application.Menu, label, accel, command string) {
		it := parent.Add(label).OnClick(func(*application.Context) { send(command) })
		if accel != "" {
			it.SetAccelerator(accel)
		}
	}
	if runtime.GOOS == "darwin" {
		appMenu := m.AddSubmenu("Hidane")
		appMenu.AddRole(application.About)
		appMenu.AddSeparator()
		item(appMenu, "Settings…", "CmdOrCtrl+,", "open-settings")
		appMenu.AddSeparator()
		appMenu.AddRole(application.ServicesMenu)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.ShowAll)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Quit)
	}
	file := m.AddSubmenu("File")
	item(file, "New Task…", "CmdOrCtrl+N", "new-task")
	item(file, "New Message", "CmdOrCtrl+L", "focus-composer")
	if runtime.GOOS != "darwin" {
		file.AddSeparator()
		item(file, "Settings…", "CmdOrCtrl+,", "open-settings")
		file.AddSeparator()
		file.AddRole(application.Quit)
	} else {
		file.AddSeparator()
		file.AddRole(application.CloseWindow)
	}
	m.AddRole(application.EditMenu)
	view := m.AddSubmenu("View")
	item(view, "Conversation", "CmdOrCtrl+1", "go:chat")
	item(view, "Tasks", "CmdOrCtrl+2", "go:items")
	item(view, "Schedules", "CmdOrCtrl+3", "go:schedules")
	item(view, "Memory", "CmdOrCtrl+4", "go:memory")
	item(view, "Worklog", "CmdOrCtrl+5", "go:log")
	view.AddSeparator()
	item(view, "Search…", "CmdOrCtrl+K", "search")
	item(view, "Toggle Sidebar", "CmdOrCtrl+B", "toggle-sidebar")
	view.AddSeparator()
	view.AddRole(application.ToggleFullscreen)
	m.AddRole(application.WindowMenu)
	return m
}

// windowState is where the window was, so the app reopens where it was left.
type windowState struct {
	X, Y, Width, Height int
}

func statePath(runtimeDir string) string { return filepath.Join(runtimeDir, "window.json") }

func loadWindowState(runtimeDir string) (windowState, bool) {
	b, err := os.ReadFile(statePath(runtimeDir))
	if err != nil {
		return windowState{}, false
	}
	var s windowState
	if json.Unmarshal(b, &s) != nil || s.Width < 600 || s.Height < 400 {
		return windowState{}, false
	}
	return s, true
}

func saveWindowState(runtimeDir string, w *application.WebviewWindow) {
	width, height := w.Size()
	x, y := w.Position()
	if width < 600 || height < 400 {
		return
	}
	b, _ := json.Marshal(windowState{X: x, Y: y, Width: width, Height: height})
	_ = os.MkdirAll(runtimeDir, 0o755)
	_ = os.WriteFile(statePath(runtimeDir), b, 0o644)
}
