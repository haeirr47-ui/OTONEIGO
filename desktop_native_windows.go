//go:build windows

package main

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

type desktopNativeStatus struct {
	TaskbarAvailable                           bool
	TaskbarState                               string
	TaskbarPositionMs, TaskbarDurationMs       int64
	NotificationRequests, NotificationAccepted int64
	LastError                                  string
}

type desktopTaskbar interface {
	setState(uintptr, uintptr) error
	setValue(uintptr, uint64, uint64) error
	release()
}

type desktopProgress struct {
	hwnd               uintptr
	state              string
	flag               uintptr
	position, duration int64
}

type desktopNativeUI struct {
	mu     sync.Mutex // Status only; never held while dispatching to the UI thread.
	status desktopNativeStatus
	closed atomic.Bool
	invoke func(func())
	create func() (desktopTaskbar, error)
	notify func(uintptr, *w32.NOTIFYICONDATA) bool
	now    func() time.Time
	// The following fields belong exclusively to the UI thread.
	taskbar    desktopTaskbar
	last       desktopProgress
	haveLast   bool
	retryAfter time.Time
}

func newDesktopNativeUI() *desktopNativeUI {
	return &desktopNativeUI{
		status: desktopNativeStatus{TaskbarState: "none"},
		invoke: application.InvokeSync, create: newDesktopTaskbar,
		notify: w32.ShellNotifyIcon, now: time.Now,
	}
}

func (n *desktopNativeUI) Status() desktopNativeStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.status
}

func nativeProgress(hwnd uintptr, playback string, position, duration int64, enabled bool) desktopProgress {
	p := desktopProgress{hwnd: hwnd, state: "none"}
	if !enabled || hwnd == 0 || playback == "" {
		return p
	}
	if playback == "loading" {
		p.state, p.flag = "indeterminate", 1
		return p
	}
	if playback == "error" {
		p.state, p.flag = "error", 4
	} else if playback == "paused" {
		p.state, p.flag = "paused", 8
	} else if playback == "playing" {
		p.state, p.flag = "normal", 2
	} else {
		return p
	}
	if duration <= 0 {
		return desktopProgress{hwnd: hwnd, state: "none"}
	}
	// Projection values are milliseconds; cap at one year and clamp position.
	const maxDuration = int64(366 * 24 * 60 * 60 * 1000)
	if duration > maxDuration {
		duration = maxDuration
	}
	if position < 0 {
		position = 0
	}
	if position > duration {
		position = duration
	}
	p.position, p.duration = position, duration
	return p
}

func (n *desktopNativeUI) UpdateTaskbar(hwnd uintptr, playback string, positionMs, durationMs int64, enabled bool) {
	if n.closed.Load() {
		return
	}
	p := nativeProgress(hwnd, playback, positionMs, durationMs, enabled)
	n.invoke(func() {
		if n.closed.Load() {
			return
		}
		if p.hwnd == 0 {
			return
		}
		if n.taskbar == nil {
			if n.now().Before(n.retryAfter) {
				return
			}
			var err error
			n.taskbar, err = n.create()
			if err != nil || n.taskbar == nil {
				n.failTaskbar()
				return
			}
		}
		if n.haveLast && n.last == p {
			return
		}
		if !n.haveLast || n.last.hwnd != p.hwnd || n.last.flag != p.flag {
			if err := n.taskbar.setState(p.hwnd, p.flag); err != nil {
				n.failTaskbar()
				return
			}
		}
		if p.duration > 0 {
			if err := n.taskbar.setValue(p.hwnd, uint64(p.position), uint64(p.duration)); err != nil {
				n.failTaskbar()
				return
			}
		}
		n.last, n.haveLast = p, true
		n.mu.Lock()
		n.status.TaskbarAvailable, n.status.TaskbarState = true, p.state
		n.status.TaskbarPositionMs, n.status.TaskbarDurationMs = p.position, p.duration
		n.mu.Unlock()
	})
}

func (n *desktopNativeUI) failTaskbar() {
	if n.taskbar != nil {
		n.taskbar.release()
		n.taskbar = nil
	}
	n.haveLast = false
	n.retryAfter = n.now().Add(30 * time.Second)
	n.mu.Lock()
	n.status.TaskbarAvailable, n.status.TaskbarState = false, "unavailable"
	n.status.TaskbarPositionMs, n.status.TaskbarDurationMs = 0, 0
	n.status.LastError = "Windows taskbar progress unavailable"
	n.mu.Unlock()
}

// fillNativeUTF16 reserves the final NUL and never splits a surrogate pair.
func fillNativeUTF16(dst []uint16, text string) {
	if len(dst) == 0 {
		return
	}
	clear(dst)
	pos := 0
	for _, r := range text {
		if r == 0 {
			break
		}
		if r > 0xffff {
			if pos+2 >= len(dst) {
				break
			}
			a, b := utf16.EncodeRune(r)
			dst[pos], dst[pos+1] = uint16(a), uint16(b)
			pos += 2
		} else {
			if pos+1 >= len(dst) {
				break
			}
			dst[pos] = uint16(r)
			pos++
		}
	}
}

func (n *desktopNativeUI) NotifyTrack(trayOwner uintptr, trayID uint32, title, body string) {
	if n.closed.Load() || trayOwner == 0 || trayID == 0 || title == "" {
		return
	}
	n.invoke(func() {
		if n.closed.Load() {
			return
		}
		nid := w32.NOTIFYICONDATA{HWnd: w32.HWND(trayOwner), UID: trayID, UFlags: w32.NIF_INFO | w32.NIF_REALTIME, DwInfoFlags: w32.NIIF_INFO | w32.NIIF_NOSOUND}
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		fillNativeUTF16(nid.SzInfoTitle[:], title)
		fillNativeUTF16(nid.SzInfo[:], body)
		accepted := n.notify(w32.NIM_MODIFY, &nid)
		n.mu.Lock()
		n.status.NotificationRequests++
		if accepted {
			n.status.NotificationAccepted++
		} else {
			n.status.LastError = "Windows tray notification request rejected"
		}
		n.mu.Unlock()
	})
}

// The controller calls Close before destroying the main window. Subsequent
// shutdown calls return before InvokeSync, even after the UI loop has stopped.
func (n *desktopNativeUI) Close() {
	if !n.closed.CompareAndSwap(false, true) {
		return
	}
	n.invoke(func() {
		if n.taskbar != nil {
			if n.haveLast {
				_ = n.taskbar.setState(n.last.hwnd, 0)
			}
			n.taskbar.release()
			n.taskbar = nil
		}
		n.mu.Lock()
		n.status.TaskbarAvailable, n.status.TaskbarState = false, "none"
		n.status.TaskbarPositionMs, n.status.TaskbarDurationMs = 0, 0
		n.mu.Unlock()
	})
}

// ITaskbarList3's first eleven slots follow the Windows SDK COM ABI. The
// pinned Wails v3.0.0 exposes creation/release but not these progress methods.
type desktopTaskbarLayout struct{ vtable *[11]uintptr }
type desktopCOMTaskbar struct {
	raw  *w32.ITaskbarList3
	call func(uintptr, ...uintptr) (uintptr, uintptr, syscall.Errno)
}

func newDesktopTaskbar() (desktopTaskbar, error) {
	// uint64 progress arguments below use the verified Windows amd64 ABI.
	if runtime.GOARCH != "amd64" {
		return nil, errors.New("unsupported taskbar ABI")
	}
	raw, err := w32.NewTaskbarList3()
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, errors.New("missing taskbar interface")
	}
	layout := (*desktopTaskbarLayout)(unsafe.Pointer(raw))
	if layout.vtable == nil || layout.vtable[2] == 0 || layout.vtable[9] == 0 || layout.vtable[10] == 0 {
		// A malformed interface must never be dereferenced for cleanup either.
		if layout.vtable != nil && layout.vtable[2] != 0 {
			raw.Release()
		}
		return nil, errors.New("missing taskbar progress methods")
	}
	return &desktopCOMTaskbar{raw: raw, call: syscall.SyscallN}, nil
}

func (t *desktopCOMTaskbar) invoke(slot int, args ...uintptr) error {
	if t.raw == nil {
		return errors.New("released taskbar")
	}
	v := (*desktopTaskbarLayout)(unsafe.Pointer(t.raw)).vtable
	if v == nil || (slot != 9 && slot != 10) || v[slot] == 0 {
		return errors.New("missing taskbar method")
	}
	params := append([]uintptr{uintptr(unsafe.Pointer(t.raw))}, args...)
	hr, _, _ := t.call(v[slot], params...)
	runtime.KeepAlive(t.raw)
	if int32(uint32(hr)) < 0 {
		return errors.New("taskbar HRESULT failure")
	}
	return nil
}
func (t *desktopCOMTaskbar) setState(hwnd, state uintptr) error { return t.invoke(10, hwnd, state) }
func (t *desktopCOMTaskbar) setValue(hwnd uintptr, completed, total uint64) error {
	return t.invoke(9, hwnd, uintptr(completed), uintptr(total))
}
func (t *desktopCOMTaskbar) release() {
	if t.raw != nil {
		t.raw.Release()
		t.raw = nil
	}
}
