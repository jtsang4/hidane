//go:build !nogui

// Package desktop is the Wails v3 shell: one window whose web content is
// served by hidane's own http.Handler through the Wails asset server, with
// live frames delivered as Wails events instead of SSE.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/jtsang4/hidane/internal/api"
	"github.com/jtsang4/hidane/internal/app"
	"github.com/jtsang4/hidane/internal/config"
)

// FrameEvent is the Wails event carrying one live frame.
const FrameEvent = "hidane:frame"

// Run opens the app and blocks until it quits.
//
// With HIDANE_GUI_SMOKE=1 the app quits as soon as the page in the webview
// has loaded and reached the backend, and fails if that does not happen in
// time: proof that the real window, asset server and API work together.
func Run(cfg *config.Config) error {
	a, err := app.Open(cfg, app.Options{LoginShell: true})
	if err != nil {
		return err
	}
	smoke := os.Getenv("HIDANE_GUI_SMOKE") == "1"
	var ready atomic.Bool
	var wapp *application.App
	var window *application.WebviewWindow
	ctx, cancel := context.WithCancel(context.Background())

	handler := a.Handler(app.HandlerOptions{
		Desktop: true,
		Open:    openWithOS,
		OnLiveHello: func() {
			wapp.Event.Emit(FrameEvent, map[string]any{"event": "hello", "data": map[string]any{"desktop": true}})
			snapshot, _, cancelLive := a.Sys.Live.Subscribe()
			cancelLive()
			for _, f := range snapshot {
				wapp.Event.Emit(FrameEvent, map[string]any{"event": "stream", "data": f})
			}
		},
		OnUIReady: func(transport string) {
			log.Printf("ui ready (live transport: %s)", transport)
			// The smoke test proves the desktop path: pushed Wails events, not
			// the SSE fallback.
			if transport != "wails" || ready.Swap(true) {
				return
			}
			if smoke {
				go func() {
					time.Sleep(300 * time.Millisecond)
					wapp.Quit()
				}()
			}
		},
	})
	wapp = application.New(application.Options{
		Name:        "hidane",
		Description: "hidane (火種) — a persistent personal agent runtime",
		Assets:      application.AssetOptions{Handler: handler, DisableLogging: true},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.jtsang4.hidane",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Restore()
					window.Show()
					window.Focus()
				}
			},
		},
		OnShutdown: func() {
			cancel()
			a.Stop()
		},
	})
	window = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "hidane 火种", Width: 1280, Height: 860, MinWidth: 720, MinHeight: 520, URL: "/",
	})

	lost, err := a.Start()
	if err != nil {
		if errors.Is(err, app.ErrLocked) {
			return fmt.Errorf("%w (is `hidane serve` running on %s?)", err, cfg.Home)
		}
		return err
	}
	if lost > 0 {
		log.Printf("%d execution(s) lost to the last restart were reported to their owners", lost)
	}
	start, _ := a.K.LatestSeq(ctx)
	go func() {
		err := api.Pump(ctx, a.K, a.Sys.Live, start, func(f api.Frame) error {
			wapp.Event.Emit(FrameEvent, map[string]any{"event": f.Event, "data": f.Data})
			return nil
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("live bridge stopped: %v", err)
		}
	}()
	if smoke {
		go func() {
			time.Sleep(90 * time.Second)
			if !ready.Load() {
				log.Printf("gui smoke: the page never reported ready")
				wapp.Quit()
			}
		}()
	}
	runErr := wapp.Run()
	a.Close()
	if runErr != nil {
		return runErr
	}
	if smoke && !ready.Load() {
		return errors.New("gui smoke failed: the webview never loaded the app")
	}
	return nil
}
