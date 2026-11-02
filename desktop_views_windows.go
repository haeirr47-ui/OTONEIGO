//go:build windows

package main

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/desktopstate"
	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type desktopViewsController struct {
	owner       *desktopController
	app         *application.App
	hub         *desktopstate.Hub
	mini        atomic.Pointer[application.WebviewWindow]
	lyrics      atomic.Pointer[application.WebviewWindow]
	exiting     atomic.Bool
	stop        chan struct{}
	stopOnce    sync.Once
	native      *desktopNativeUI
	save        func(string, domain.WindowPlacement) error
	timerMu     sync.Mutex
	timers      map[string]*time.Timer
	generation  map[string]uint64
	saveMu      sync.Mutex
	miniReady   atomic.Bool
	lyricsReady atomic.Bool
}

func (d *desktopController) ConfigureViews(app *application.App, save func(string, domain.WindowPlacement) error) {
	v := &desktopViewsController{owner: d, app: app, hub: desktopstate.New(d.settings().Desktop), save: save, stop: make(chan struct{}), native: newDesktopNativeUI(), timers: map[string]*time.Timer{}, generation: map[string]uint64{}}
	d.views = v
	app.Event.On("otoneigo:desktop-publish", func(e *application.CustomEvent) {
		v.hub.Settings(d.settings().Desktop)
		s, notify, err := v.hub.Publish(e.Sender, e.Data)
		if err != nil {
			return
		}
		v.broadcast(s)
		v.projectNative(s, notify)
	})
	app.Event.On("otoneigo:desktop-ready", func(e *application.CustomEvent) {
		if kind, ok := e.Data.(string); ok && kind != e.Sender {
			return
		}
		if w := v.get(e.Sender); w != nil {
			emitDesktopEvent(w, "otoneigo:desktop-snapshot", v.Snapshot())
		}
	})
	d.window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		epoch := v.hub.Navigate()
		emitDesktopEvent(d.window, "otoneigo:desktop-epoch", epoch)
		v.broadcast(v.Snapshot())
	})
	go v.refresh()
}
func (d *desktopController) Views() *desktopViewsController {
	if d == nil {
		return nil
	}
	return d.views
}
func (d *desktopController) MarkMainClosing() {
	if d != nil && d.views != nil {
		d.views.hub.Close()
		d.views.broadcast(d.views.Snapshot())
	}
}
func (d *desktopController) CloseAuxiliaryWindows() {
	if d != nil && d.views != nil {
		v := d.views
		v.exiting.Store(true)
		v.hub.Close()
		// The root closing hook is a Go window-event handler. Read native DIP
		// geometry first, then commit outside UI dispatch before store shutdown.
		v.flushPositions()
		v.native.Close()
		for _, w := range []*application.WebviewWindow{v.mini.Load(), v.lyrics.Load()} {
			if w != nil {
				w.Close()
			}
		}
	}
}
func (d *desktopController) ApplyDesktopSettings() {
	if d == nil || d.views == nil {
		return
	}
	// Settings notifications are drained outside AppService's commit mutex.
	// Queue UI work so concurrent native geometry callbacks remain responsive.
	go func() {
		v := d.views
		if v.exiting.Load() || d.closed.Load() {
			return
		}
		application.InvokeSync(func() {
			if v.exiting.Load() || d.closed.Load() {
				return
			}
			s := d.settings().Desktop
			v.hub.Settings(s)
			if w := v.mini.Load(); w != nil {
				w.SetAlwaysOnTop(s.MiniAlwaysOnTop)
			}
			if w := v.lyrics.Load(); w != nil {
				w.SetAlwaysOnTop(s.LyricsAlwaysOnTop)
				w.SetIgnoreMouseEvents(s.LyricsLocked)
			}
			v.broadcast(v.Snapshot())
			v.projectNative(v.Snapshot(), false)
		})
	}()
}
func (v *desktopViewsController) Epoch() int64 { return v.hub.Epoch() }
func (v *desktopViewsController) Snapshot() domain.DesktopSnapshot {
	if v.owner != nil && v.owner.settings != nil {
		v.hub.Settings(v.owner.settings().Desktop)
	}
	return v.hub.Snapshot()
}
func (v *desktopViewsController) get(kind string) *application.WebviewWindow {
	switch kind {
	case "mini":
		return v.mini.Load()
	case "lyrics":
		return v.lyrics.Load()
	}
	return nil
}
func (v *desktopViewsController) broadcast(s domain.DesktopSnapshot) {
	for _, w := range []*application.WebviewWindow{v.mini.Load(), v.lyrics.Load()} {
		if w != nil {
			emitDesktopEvent(w, "otoneigo:desktop-snapshot", s)
		}
	}
}
func (v *desktopViewsController) Dispatch(c domain.DesktopCommand) error {
	var err error
	application.InvokeSync(func() {
		err = v.hub.Command(c)
		if err == nil {
			emitDesktopEvent(v.owner.window, "otoneigo:desktop-command", c)
		}
	})
	return err
}
func (v *desktopViewsController) ShowView(kind string) error {
	if kind != "mini" && kind != "lyrics" {
		return errors.New("INVALID_DESKTOP_VIEW")
	}
	if v.Snapshot().Closing || v.exiting.Load() {
		return errors.New("DESKTOP_CLOSING")
	}
	application.InvokeSync(func() {
		if v.exiting.Load() {
			return
		}
		w := v.get(kind)
		if w == nil {
			s := v.owner.settings().Desktop
			p := s.MiniPlacement
			title := "OTONEIGO迷你播放器"
			top := s.MiniAlwaysOnTop
			bg := application.BackgroundTypeSolid
			if kind == "lyrics" {
				p = s.LyricsPlacement
				title = "桌面歌词"
				top = s.LyricsAlwaysOnTop
				bg = application.BackgroundTypeTransparent
			}
			p = v.clamp(p)
			w = v.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: kind, Title: title, URL: "/?view=" + kind, InitialPosition: application.WindowXY, Width: p.Width, Height: p.Height, X: p.X, Y: p.Y, Hidden: true, Frameless: true, AlwaysOnTop: top, BackgroundType: bg, MinWidth: 240, MinHeight: 100})
			if w == nil {
				return
			}
			if kind == "mini" {
				v.mini.Store(w)
			} else {
				v.lyrics.Store(w)
				w.SetIgnoreMouseEvents(s.LyricsLocked)
			}
			w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
				if !v.exiting.Load() {
					e.Cancel()
					_ = v.HideView(kind)
				}
			})
			w.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) { v.scheduleSave(kind, w) })
			w.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { v.scheduleSave(kind, w) })
			w.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
				if kind == "mini" {
					v.miniReady.Store(true)
				} else {
					v.lyricsReady.Store(true)
				}
			})
		}
		w.Show()
		emitDesktopEvent(w, "otoneigo:desktop-snapshot", v.Snapshot())
	})
	if v.get(kind) == nil {
		v.owner.Show()
		return errors.New("DESKTOP_VIEW_UNAVAILABLE")
	}
	return nil
}
func (v *desktopViewsController) HideView(kind string) error {
	if kind != "mini" && kind != "lyrics" {
		return errors.New("INVALID_DESKTOP_VIEW")
	}
	if w := v.get(kind); w != nil {
		v.scheduleSave(kind, w)
		w.Hide()
	}
	if !v.owner.Available() && !v.owner.window.IsVisible() {
		v.owner.Show()
	}
	return nil
}
func (v *desktopViewsController) clamp(p domain.WindowPlacement) domain.WindowPlacement {
	areas := []domain.WindowPlacement{}
	for _, s := range v.app.Screen.GetAll() {
		r := s.WorkArea
		areas = append(areas, domain.WindowPlacement{Valid: true, X: r.X, Y: r.Y, Width: r.Width, Height: r.Height})
	}
	return desktopstate.ClampPlacement(p, areas)
}
func (v *desktopViewsController) ResetPositions() error {
	if v.Snapshot().Closing {
		return errors.New("DESKTOP_CLOSING")
	}
	defs := domain.DefaultDesktopSettings()
	for _, item := range []struct {
		kind string
		p    domain.WindowPlacement
	}{{"mini", defs.MiniPlacement}, {"lyrics", defs.LyricsPlacement}} {
		v.invalidateSave(item.kind)
		v.saveMu.Lock()
		if v.save != nil {
			if err := v.save(item.kind, item.p); err != nil {
				v.saveMu.Unlock()
				return err
			}
		}
		v.saveMu.Unlock()
		if w := v.get(item.kind); w != nil {
			p := v.clamp(item.p)
			w.SetBounds(application.Rect{X: p.X, Y: p.Y, Width: p.Width, Height: p.Height})
		}
	}
	return nil
}
func (v *desktopViewsController) invalidateSave(kind string) {
	v.timerMu.Lock()
	defer v.timerMu.Unlock()
	v.generation[kind]++
	if t := v.timers[kind]; t != nil {
		t.Stop()
	}
}
func (v *desktopViewsController) scheduleSave(kind string, w *application.WebviewWindow) {
	if v.exiting.Load() || v.save == nil || !w.IsVisible() {
		return
	}
	if (kind == "mini" && !v.miniReady.Load()) || (kind == "lyrics" && !v.lyricsReady.Load()) {
		return
	}
	r := w.Bounds()
	p := domain.WindowPlacement{Valid: true, X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
	if domain.ValidateWindowPlacement(p) != nil {
		return
	}
	v.enqueuePlacement(kind, p)
}
func (v *desktopViewsController) enqueuePlacement(kind string, p domain.WindowPlacement) {
	v.timerMu.Lock()
	defer v.timerMu.Unlock()
	v.generation[kind]++
	g := v.generation[kind]
	if t := v.timers[kind]; t != nil {
		t.Stop()
	}
	v.timers[kind] = time.AfterFunc(350*time.Millisecond, func() {
		v.saveMu.Lock()
		defer v.saveMu.Unlock()
		v.timerMu.Lock()
		current := v.generation[kind] == g
		v.timerMu.Unlock()
		if current && !v.exiting.Load() {
			_ = v.save(kind, p)
		}
	})
}
func (v *desktopViewsController) flushPositions() {
	if v.save == nil {
		return
	}
	for _, kind := range []string{"mini", "lyrics"} {
		v.invalidateSave(kind)
		w := v.get(kind)
		if w == nil || (kind == "mini" && !v.miniReady.Load()) || (kind == "lyrics" && !v.lyricsReady.Load()) {
			continue
		}
		r := w.Bounds()
		p := domain.WindowPlacement{Valid: true, X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}
		if domain.ValidateWindowPlacement(p) != nil {
			continue
		}
		v.saveMu.Lock()
		_ = v.save(kind, p)
		v.saveMu.Unlock()
	}
}
func (v *desktopViewsController) refresh() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-v.stop:
			return
		case <-ticker.C:
			if v.Snapshot().Closing {
				continue
			}
			visible := false
			for _, w := range []*application.WebviewWindow{v.mini.Load(), v.lyrics.Load()} {
				if w != nil && w.IsVisible() {
					visible = true
				}
			}
			if visible {
				emitDesktopEvent(v.owner.window, "otoneigo:desktop-refresh", v.Epoch())
			}
		}
	}
}

// EmitEvent broadcasts globally in the pinned Wails release. DispatchWailsEvent
// targets the selected WebView and does not invoke application event listeners.
func emitDesktopEvent(w *application.WebviewWindow, name string, data any) {
	w.DispatchWailsEvent(&application.CustomEvent{Name: name, Data: data})
}
func (v *desktopViewsController) close() {
	v.stopOnce.Do(func() {
		close(v.stop)
		v.hub.Close()
		v.exiting.Store(true)
		v.timerMu.Lock()
		for _, t := range v.timers {
			t.Stop()
		}
		v.timerMu.Unlock()
		v.native.Close()
	})
}
func desktopCaption(s domain.DesktopSnapshot) string {
	if s.Title == "" {
		return "OTONEIGO"
	}
	title := strings.ReplaceAll(strings.ReplaceAll(s.Title, "\r", " "), "\n", " ")
	if s.Artist != "" {
		title += " · " + strings.ReplaceAll(strings.ReplaceAll(s.Artist, "\r", " "), "\n", " ")
	}
	return title + " — OTONEIGO"
}
func (v *desktopViewsController) projectNative(s domain.DesktopSnapshot, notify bool) {
	application.InvokeSync(func() {
		if v.exiting.Load() || v.owner.closed.Load() {
			return
		}
		current := v.Snapshot()
		if current.Closing || current.Epoch != s.Epoch {
			return
		}
		// A following progress publication may overtake UI dispatch. Retain the
		// one initial notification decision for the same entry while presenting
		// the latest projection; never let an old entry overwrite a newer one.
		notify = notify && current.QueueEntryID == s.QueueEntryID
		s = current
		v.owner.window.SetTitle(desktopCaption(s))
		v.native.UpdateTaskbar(uintptr(v.owner.window.NativeWindow()), s.Playback, s.PositionMs, s.DurationMs, s.Options.TaskbarProgress)
		if notify && s.Options.NotifyTrackChanges && (!v.owner.window.IsVisible() || v.owner.window.IsMinimised()) {
			if hwnd := desktopTrayWindow(v.app.Config().Windows.WndClass); hwnd != 0 {
				v.native.NotifyTrack(hwnd, 1, s.Title, s.Artist)
			}
		}
	})
}
func (v *desktopViewsController) State() domain.DesktopNativeState {
	n := v.native.Status()
	s := domain.DesktopNativeState{MainVisible: v.owner.window.IsVisible(), MainMinimized: v.owner.window.IsMinimised(), LyricsLocked: v.owner.settings().Desktop.LyricsLocked, TaskbarAvailable: n.TaskbarAvailable, TaskbarState: n.TaskbarState, TaskbarPositionMs: n.TaskbarPositionMs, TaskbarDurationMs: n.TaskbarDurationMs, NotificationRequests: n.NotificationRequests, NotificationAccepted: n.NotificationAccepted, LastError: n.LastError}
	if w := v.mini.Load(); w != nil {
		s.MiniVisible = w.IsVisible()
	}
	if w := v.lyrics.Load(); w != nil {
		s.LyricsVisible = w.IsVisible()
	}
	return s
}
func desktopTrayWindow(windowClass string) uintptr {
	class, e := windows.UTF16PtrFromString(windowClass)
	if e != nil || windowClass == "" {
		return 0
	}
	var after uintptr
	for range 256 {
		hwnd, _, _ := desktopFindWindowEx.Call(^uintptr(2), after, uintptr(unsafe.Pointer(class)), 0)
		if hwnd == 0 {
			return 0
		}
		after = hwnd
		_, pid := w32.GetWindowThreadProcessId(w32.HWND(hwnd))
		if pid == os.Getpid() {
			return hwnd
		}
	}
	return 0
}
