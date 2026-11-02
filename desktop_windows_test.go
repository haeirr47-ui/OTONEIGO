//go:build windows

package main

import (
	"bytes"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/lifecycle"
)

func TestOverlappingCloseDecisionsAreSerialized(t *testing.T) {
	var preference atomic.Bool
	var reads atomic.Int32
	var hides atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	d := &desktopController{trayAvailable: func() bool { return true }, settings: func() domain.LocalSettings {
		if reads.Add(1) == 1 {
			close(entered)
			<-release
		}
		s := domain.DefaultSettings()
		s.CloseToTray = preference.Load()
		return s
	}}
	gate := lifecycle.NewCloseGate(time.Hour, func() {})
	defer gate.Stop()
	c := &windowCloseCoordinator{gate: gate, desktop: d}
	doneA, startedB, doneB := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { c.Request(func() { hides.Add(1) }); close(doneA) }()
	<-entered
	go func() { close(startedB); c.Request(func() { hides.Add(1) }); close(doneB) }()
	<-startedB
	select {
	case <-doneB:
		t.Fatal("overlapping close bypassed the pending decision")
	case <-time.After(30 * time.Millisecond):
	}
	if gate.Started() {
		t.Fatal("normal close started outside decision lock")
	}
	preference.Store(true)
	close(release)
	<-doneA
	<-doneB
	if gate.Started() || hides.Load() != 2 {
		t.Fatal("serialized hides unexpectedly began normal close", hides.Load())
	}
}

func TestNormalCloseDoesNotBecomeHiddenAfterPreferenceSave(t *testing.T) {
	for _, availabilityChanges := range []bool{false, true} {
		settings := domain.DefaultSettings()
		settings.CloseToTray = availabilityChanges
		available := !availabilityChanges
		d := &desktopController{settings: func() domain.LocalSettings { return settings }, trayAvailable: func() bool { return available }}
		closed := false
		gate := lifecycle.NewCloseGate(time.Hour, func() { closed = true })
		if shouldHideWindowOnClose(gate, d) {
			t.Fatal("initial close should prepare normal exit")
		}
		allow, token := gate.Request()
		if allow || !gate.Started() {
			t.Fatal("normal intent was not latched")
		}
		settings.CloseToTray = true
		available = true
		if shouldHideWindowOnClose(gate, d) {
			t.Fatal("pending save changed frozen exit into a hidden window")
		}
		gate.Acknowledge(token)
		if !closed || shouldHideWindowOnClose(gate, d) {
			t.Fatal("acknowledged exit was redirected to tray")
		}
		gate.Stop()
	}
}

func TestDesktopCloseToTrayRequiresPreferenceAndReachableTray(t *testing.T) {
	settings := domain.DefaultSettings()
	available := true
	d := &desktopController{settings: func() domain.LocalSettings { return settings }, trayAvailable: func() bool { return available }}
	if d.WantsHideOnClose() {
		t.Fatal("default close must exit")
	}
	settings.CloseToTray = true
	if !d.WantsHideOnClose() {
		t.Fatal("enabled close with reachable tray must hide")
	}
	available = false
	if d.WantsHideOnClose() {
		t.Fatal("unreachable tray must preserve normal close")
	}
	available = true
	d.quitRequested.Store(true)
	if d.WantsHideOnClose() {
		t.Fatal("explicit quit must pass through the close gate")
	}
	d.quitRequested.Store(false)
	d.Close()
	d.Close()
	if d.Available() || d.WantsHideOnClose() {
		t.Fatal("destroyed controller must never hide")
	}
}

func TestTrayUnavailabilityReportsOnlyTransitions(t *testing.T) {
	var available atomic.Bool
	var reported atomic.Int32
	d := &desktopController{trayAvailable: available.Load, onTrayUnavailable: func() { reported.Add(1) }}
	for range 50 {
		if d.Available() {
			t.Fatal("unavailable tray reported available")
		}
	}
	if reported.Load() != 1 {
		t.Fatal("unavailable state not deduplicated", reported.Load())
	}
	available.Store(true)
	if !d.Available() {
		t.Fatal("tray recovery not detected")
	}
	available.Store(false)
	_ = d.Available()
	if reported.Load() != 2 {
		t.Fatal("second outage not observable", reported.Load())
	}
	d.closed.Store(true)
	_ = d.Available()
	if reported.Load() != 2 {
		t.Fatal("shutdown reported an outage")
	}
}

func TestDesktopIconPreservesBrandGeometry(t *testing.T) {
	data, err := desktopIcon()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatal("unexpected icon dimensions")
	}
	for _, p := range []struct {
		x, y  int
		black bool
	}{{32, 12, true}, {12, 32, true}, {37, 32, true}, {25, 32, false}, {0, 0, false}} {
		r, g, b, a := img.At(p.x, p.y).RGBA()
		black := r < 0x3000 && g < 0x3000 && b < 0x3000
		if black != p.black || a != 0xffff {
			t.Errorf("wrong brand pixel at %d,%d", p.x, p.y)
		}
	}
}
