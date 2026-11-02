//go:build windows

package main

import (
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

type fakeDesktopTaskbar struct {
	states   [][2]uintptr
	values   [][3]uint64
	releases int
	fail     bool
	check    func()
}

func (f *fakeDesktopTaskbar) setState(hwnd, state uintptr) error {
	if f.check != nil {
		f.check()
	}
	f.states = append(f.states, [2]uintptr{hwnd, state})
	if f.fail {
		return errors.New("injected failure")
	}
	return nil
}
func (f *fakeDesktopTaskbar) setValue(hwnd uintptr, pos, total uint64) error {
	if f.check != nil {
		f.check()
	}
	f.values = append(f.values, [3]uint64{uint64(hwnd), pos, total})
	if f.fail {
		return errors.New("injected failure")
	}
	return nil
}
func (f *fakeDesktopTaskbar) release() {
	if f.check != nil {
		f.check()
	}
	f.releases++
}

func TestDesktopNativeDispatchBoundary(t *testing.T) {
	inside := false
	check := func() {
		if !inside {
			t.Fatal("native API escaped UI dispatch")
		}
	}
	f := &fakeDesktopTaskbar{check: check}
	n := testDesktopNative(f)
	n.invoke = func(fn func()) {
		// Status remains callable during dispatch: no status lock is held by caller.
		_ = n.Status()
		inside = true
		defer func() { inside = false }()
		fn()
	}
	n.create = func() (desktopTaskbar, error) { check(); return f, nil }
	n.notify = func(uintptr, *w32.NOTIFYICONDATA) bool { check(); return true }
	n.UpdateTaskbar(42, "playing", 1, 10, true)
	n.NotifyTrack(42, 1, "title", "body")
	n.Close()
}

func testDesktopNative(f *fakeDesktopTaskbar) *desktopNativeUI {
	n := newDesktopNativeUI()
	n.invoke = func(fn func()) { fn() }
	n.create = func() (desktopTaskbar, error) { return f, nil }
	return n
}

func TestDesktopNativeProgressMapping(t *testing.T) {
	for _, tc := range []struct {
		name, playback, state string
		pos, duration         int64
		enabled               bool
		flag                  uintptr
		wantPos, wantDuration int64
	}{
		{"disabled", "playing", "none", 4, 10, false, 0, 0, 0},
		{"no track", "", "none", 4, 10, true, 0, 0, 0},
		{"loading", "loading", "indeterminate", 4, 10, true, 1, 0, 0},
		{"playing", "playing", "normal", 4, 10, true, 2, 4, 10},
		{"negative", "paused", "paused", -4, 10, true, 8, 0, 10},
		{"overrun", "error", "error", 40, 10, true, 4, 10, 10},
		{"unknown duration", "playing", "none", 4, 0, true, 0, 0, 0},
		{"unknown state", "bad", "none", 4, 10, true, 0, 0, 0},
		{"duration cap", "playing", "normal", 1 << 62, 1 << 62, true, 2, 31622400000, 31622400000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := nativeProgress(42, tc.playback, tc.pos, tc.duration, tc.enabled)
			if p.state != tc.state || p.flag != tc.flag || p.position != tc.wantPos || p.duration != tc.wantDuration {
				t.Fatalf("unexpected progress: %+v", p)
			}
		})
	}
}

func TestDesktopNativeDedupAndCleanup(t *testing.T) {
	f := &fakeDesktopTaskbar{}
	n := testDesktopNative(f)
	if n.Status().TaskbarAvailable {
		t.Fatal("factory must be lazy")
	}
	n.UpdateTaskbar(42, "playing", 3, 10, true)
	n.UpdateTaskbar(42, "playing", 3, 10, true)
	n.UpdateTaskbar(42, "playing", 4, 10, true)
	if len(f.states) != 1 || len(f.values) != 2 {
		t.Fatalf("unexpected redundant calls: %+v", f)
	}
	n.UpdateTaskbar(42, "paused", 4, 10, true)
	if f.states[1][1] != 8 {
		t.Fatal("pause must use TBPF_PAUSED")
	}
	n.UpdateTaskbar(42, "playing", 4, 10, false)
	if f.states[2][1] != 0 {
		t.Fatal("disable must clear progress")
	}
	n.Close()
	if f.releases != 1 || n.Status().TaskbarState != "none" {
		t.Fatal("close did not release/reset")
	}
	n.invoke = func(func()) { t.Fatal("closed helper dispatched after UI loop stopped") }
	n.Close()
	n.UpdateTaskbar(42, "playing", 4, 10, true)
	n.NotifyTrack(42, 1, "title", "body")
}

func TestDesktopNativeFailureCooldownAndRecovery(t *testing.T) {
	f := &fakeDesktopTaskbar{fail: true}
	n := testDesktopNative(f)
	now := time.Unix(100, 0)
	n.now = func() time.Time { return now }
	creates := 0
	n.create = func() (desktopTaskbar, error) { creates++; return f, nil }
	n.UpdateTaskbar(42, "playing", 3, 10, true)
	for i := 0; i < 100; i++ {
		n.UpdateTaskbar(42, "playing", int64(i), 100, true)
	}
	if creates != 1 || f.releases != 1 || n.Status().TaskbarState != "unavailable" {
		t.Fatalf("failure not bounded: creates=%d status=%+v", creates, n.Status())
	}
	now = now.Add(30 * time.Second)
	f.fail = false
	n.UpdateTaskbar(42, "playing", 3, 10, true)
	if creates != 2 || !n.Status().TaskbarAvailable {
		t.Fatal("did not retry after cooldown")
	}
	n.Close()
}

func TestDesktopNativeNotificationRequest(t *testing.T) {
	n := testDesktopNative(&fakeDesktopTaskbar{})
	calls := 0
	n.notify = func(command uintptr, data *w32.NOTIFYICONDATA) bool {
		calls++
		if command != w32.NIM_MODIFY || data.HWnd != w32.HWND(42) || data.UID != 1 {
			t.Fatal("notification must modify the existing owner/icon")
		}
		if data.CbSize != uint32(unsafe.Sizeof(*data)) || data.UFlags != w32.NIF_INFO|w32.NIF_REALTIME || data.DwInfoFlags != w32.NIIF_INFO|w32.NIIF_NOSOUND {
			t.Fatal("unexpected notification flags/size")
		}
		if data.HIcon != 0 || data.UCallbackMessage != 0 || data.SzTip[0] != 0 {
			t.Fatal("notification must not replace tray identity or callback")
		}
		if syscall.UTF16ToString(data.SzInfoTitle[:]) != "title" || syscall.UTF16ToString(data.SzInfo[:]) != "body" {
			t.Fatal("incorrect text")
		}
		return calls == 1
	}
	n.NotifyTrack(0, 1, "title", "body")
	n.NotifyTrack(42, 0, "title", "body")
	n.NotifyTrack(42, 1, "", "body")
	n.NotifyTrack(42, 1, "title", "body")
	n.NotifyTrack(42, 1, "title", "body")
	s := n.Status()
	if calls != 2 || s.NotificationRequests != 2 || s.NotificationAccepted != 1 || s.LastError != "Windows tray notification request rejected" {
		t.Fatalf("unexpected counters/status: %+v", s)
	}
}

func TestDesktopNativeUTF16Bounds(t *testing.T) {
	for _, size := range []int{1, 2, 3, 64, 256} {
		dst := make([]uint16, size)
		fillNativeUTF16(dst, strings.Repeat("🎵", 256))
		if dst[len(dst)-1] != 0 {
			t.Fatal("missing terminator")
		}
		text := syscall.UTF16ToString(dst)
		if strings.ContainsRune(text, '\uFFFD') {
			t.Fatal("truncated surrogate pair")
		}
	}
	dst := make([]uint16, 8)
	fillNativeUTF16(dst, "ab\x00cd")
	if syscall.UTF16ToString(dst) != "ab" {
		t.Fatal("embedded NUL not bounded")
	}
}

func TestDesktopNativeCOMProgressABI(t *testing.T) {
	vt := &[11]uintptr{9: 109, 10: 110}
	layout := &desktopTaskbarLayout{vtable: vt}
	raw := (*w32.ITaskbarList3)(unsafe.Pointer(layout))
	var calls [][]uintptr
	hr := uintptr(0)
	taskbar := &desktopCOMTaskbar{raw: raw, call: func(fn uintptr, args ...uintptr) (uintptr, uintptr, syscall.Errno) {
		calls = append(calls, append([]uintptr{fn}, args...))
		return hr, 0, 0
	}}
	if err := taskbar.setState(42, 8); err != nil {
		t.Fatal(err)
	}
	if err := taskbar.setValue(42, 3, 10); err != nil {
		t.Fatal(err)
	}
	this := uintptr(unsafe.Pointer(raw))
	want := [][]uintptr{{110, this, 42, 8}, {109, this, 42, 3, 10}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("incorrect COM slot or this pointer: %v", calls)
	}
	hr = 1 // Successful nonzero HRESULT must not be treated as failure.
	if err := taskbar.setState(42, 0); err != nil {
		t.Fatal(err)
	}
	hr = 0x80004005
	if err := taskbar.setValue(42, 3, 10); err == nil {
		t.Fatal("failed HRESULT accepted")
	}
	vt[9] = 0
	before := len(calls)
	if err := taskbar.setValue(42, 3, 10); err == nil || len(calls) != before {
		t.Fatal("called missing COM function")
	}
}
