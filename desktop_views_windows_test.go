//go:build windows

package main

import (
	"github.com/voici5986/otoneigo/internal/desktopstate"
	"github.com/voici5986/otoneigo/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestDesktopCaptionUsesProjectedMetadata(t *testing.T) {
	if got := desktopCaption(domain.DesktopSnapshot{}); got != "OTONEIGO" {
		t.Fatal(got)
	}
	got := desktopCaption(domain.DesktopSnapshot{Title: "Song\nName", Artist: "Artist\rName"})
	if !strings.HasSuffix(got, " — OTONEIGO") || strings.ContainsAny(got, "\r\n") {
		t.Fatal(got)
	}
}
func TestDesktopMainClosingFreezesHub(t *testing.T) {
	h := desktopstate.New(domain.DefaultDesktopSettings())
	d := &desktopController{views: &desktopViewsController{hub: h}}
	d.MarkMainClosing()
	if !h.Snapshot().Closing || h.Snapshot().Ready {
		t.Fatal(h.Snapshot())
	}
	d.MarkMainClosing()
	if h.Navigate() != 1 {
		t.Fatal("closing advanced epoch")
	}
}
func TestDesktopPlacementDebounceAndGeneration(t *testing.T) {
	saved := make(chan domain.WindowPlacement, 4)
	v := &desktopViewsController{timers: map[string]*time.Timer{}, generation: map[string]uint64{}, save: func(_ string, p domain.WindowPlacement) error { saved <- p; return nil }}
	p := domain.DefaultDesktopSettings().MiniPlacement
	p.Valid = true
	p.X = 1
	v.enqueuePlacement("mini", p)
	p.X = 2
	v.enqueuePlacement("mini", p)
	select {
	case got := <-saved:
		if got.X != 2 {
			t.Fatal("older geometry won", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("debounce did not save")
	}
	p.X = 3
	v.enqueuePlacement("mini", p)
	v.invalidateSave("mini")
	select {
	case got := <-saved:
		t.Fatal("reset generation allowed stale pending save", got)
	case <-time.After(450 * time.Millisecond):
	}
	v.exiting.Store(true)
	p.X = 4
	v.enqueuePlacement("mini", p)
	select {
	case got := <-saved:
		t.Fatal("exit allowed pending save", got)
	case <-time.After(450 * time.Millisecond):
	}
}
func TestDesktopPlacementResetWaitsForInflightSave(t *testing.T) {
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	v := &desktopViewsController{timers: map[string]*time.Timer{}, generation: map[string]uint64{}, save: func(_ string, p domain.WindowPlacement) error { close(started); <-release; return nil }}
	v.enqueuePlacement("mini", domain.DefaultDesktopSettings().MiniPlacement)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("save not entered")
	}
	v.invalidateSave("mini")
	go func() { v.saveMu.Lock(); v.saveMu.Unlock(); close(finished) }()
	select {
	case <-finished:
		t.Fatal("reset bypassed inflight placement commit")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("inflight save did not release")
	}
}
