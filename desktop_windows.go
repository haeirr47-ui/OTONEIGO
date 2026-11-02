//go:build windows

package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

type desktopController struct {
	window            *application.WebviewWindow
	tray              *application.SystemTray
	settings          func() domain.LocalSettings
	trayAvailable     func() bool
	onTrayUnavailable func()
	trayState         atomic.Int32
	quitRequested     atomic.Bool
	closed            atomic.Bool
	closeOnce         sync.Once
	views             *desktopViewsController
}

func newDesktopController(app *application.App, window *application.WebviewWindow, settings func() domain.LocalSettings) (*desktopController, error) {
	if app == nil || window == nil || settings == nil {
		return nil, errors.New("DESKTOP_UNAVAILABLE")
	}
	icon, err := desktopIcon()
	if err != nil {
		return nil, err
	}
	d := &desktopController{window: window, settings: settings, trayAvailable: func() bool { return desktopTrayAvailable(app.Config().Windows.WndClass) }}
	d.tray = app.SystemTray.New()
	d.tray.SetIcon(icon)
	d.tray.SetTooltip("OTONEIGO")
	menu := app.NewMenu()
	for _, action := range []struct{ label, command string }{
		{"显示 OTONEIGO", "show"}, {"隐藏窗口", "hide"},
		{"播放 / 暂停", "toggle-play"}, {"上一首", "previous"}, {"下一首", "next"},
		{"迷你播放器", "show-mini"}, {"桌面歌词", "show-lyrics"}, {"锁定 / 解锁歌词", "toggle-lyrics-lock"},
	} {
		command := action.command
		menu.Add(action.label).OnClick(func(*application.Context) { d.Dispatch(command) })
	}
	menu.AddSeparator()
	menu.Add("退出 OTONEIGO").OnClick(func(*application.Context) { d.Dispatch("quit") })
	d.tray.SetMenu(menu)
	d.tray.OnClick(d.Show)
	return d, nil
}

func (d *desktopController) Available() bool {
	if d == nil || d.closed.Load() {
		return false
	}
	available := d.trayAvailable != nil && d.trayAvailable()
	if available {
		d.trayState.Store(1)
	} else if d.trayState.Swap(2) != 2 && d.onTrayUnavailable != nil {
		d.onTrayUnavailable()
	}
	return available
}

func (d *desktopController) WantsHideOnClose() bool {
	return d != nil && !d.closed.Load() && !d.quitRequested.Load() && d.settings != nil && d.settings().CloseToTray && d.Available()
}

func (d *desktopController) Hide() {
	// An unavailable shell must never strand the application's only window.
	if d != nil && !d.closed.Load() && !d.quitRequested.Load() && d.Available() {
		d.window.Hide()
	}
}

func (d *desktopController) Show() {
	if d == nil || d.closed.Load() {
		return
	}
	d.window.Show()
	if d.window.IsMinimised() {
		d.window.UnMinimise()
	}
	d.window.Focus()
}

func (d *desktopController) RequestQuit() {
	if d == nil || d.closed.Load() {
		return
	}
	d.quitRequested.Store(true)
	// Go through WindowClosing and its existing snapshot flush/timeout gate.
	d.window.Close()
}

func (d *desktopController) Dispatch(command string) {
	if d == nil || d.closed.Load() {
		return
	}
	switch command {
	case "show":
		d.Show()
	case "hide":
		d.Hide()
	case "quit":
		d.RequestQuit()
	case "show-mini", "show-lyrics":
		if d.views != nil {
			_ = d.views.ShowView(command[5:])
		}
	case "toggle-lyrics-lock":
		if d.views != nil {
			_ = d.views.Dispatch(domain.DesktopCommand{Epoch: d.views.Epoch(), Command: command})
		}
	case "toggle-play", "previous", "next":
		if !d.quitRequested.Load() {
			if d.views != nil {
				_ = d.views.Dispatch(domain.DesktopCommand{Epoch: d.views.Epoch(), Command: command})
			} else {
				d.window.EmitEvent("otoneigo:player-command", command)
			}
		}
	}
}

func (d *desktopController) Close() {
	if d == nil {
		return
	}
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		if d.views != nil {
			d.views.close()
		}
		if d.tray != nil {
			d.tray.Destroy()
		}
	})
}

var desktopFindWindowEx = windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowExW")

// Wails v3.0.0 exposes no tray readiness API. Its sole tray uses ID 1 and a
// message-only window. Query only this process's windows, without changing them.
// Failure (including shell restart/overflow lookup failure) leaves normal close
// behavior in place. Keep this bounded adapter aligned with the pinned Wails API.
func desktopTrayAvailable(windowClass string) bool {
	className, err := windows.UTF16PtrFromString(windowClass)
	if err != nil || windowClass == "" {
		return false
	}
	const hwndMessage = ^uintptr(2) // HWND_MESSAGE (-3)
	var after uintptr
	for range 256 {
		hwnd, _, _ := desktopFindWindowEx.Call(hwndMessage, after, uintptr(unsafe.Pointer(className)), 0)
		if hwnd == 0 {
			return false
		}
		after = hwnd
		_, pid := w32.GetWindowThreadProcessId(w32.HWND(hwnd))
		if pid != os.Getpid() {
			continue
		}
		bounds, err := w32.GetSystrayBounds(w32.HWND(hwnd), 1)
		if err == nil && bounds != nil && bounds.Right > bounds.Left && bounds.Bottom > bounds.Top {
			return true
		}
	}
	return false
}

// Raster adaptation of frontend/public/logo.svg, reused from OTONEI under MIT
// (see frontend/THIRD_PARTY_NOTICES.md). The geometry remains its circle and bar;
// an opaque cream field keeps the original black mark legible in both taskbars.
func desktopIcon() ([]byte, error) {
	const size, samples = 64, 4
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			hits := 0
			for sy := range samples {
				for sx := range samples {
					px := float64(x) + (float64(sx)+0.5)/samples
					py := float64(y) + (float64(sy)+0.5)/samples
					ring := math.Abs(math.Hypot(px-32, py-32)-20) <= 1.5
					nearestY := math.Max(14.5, math.Min(49.5, py))
					bar := math.Hypot(px-37, py-nearestY) <= 1.5
					if ring || bar {
						hits++
					}
				}
			}
			blend := func(background int) uint8 { return uint8(background * (samples*samples - hits) / (samples * samples)) }
			img.SetNRGBA(x, y, color.NRGBA{R: blend(250), G: blend(247), B: blend(240), A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
