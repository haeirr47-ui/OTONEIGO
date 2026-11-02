package appservice

import (
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
)

type testDesktopViews struct {
	kind    string
	command domain.DesktopCommand
	reset   bool
}

func (d *testDesktopViews) Epoch() int64                     { return 7 }
func (d *testDesktopViews) Snapshot() domain.DesktopSnapshot { return domain.DesktopSnapshot{Epoch: 7} }
func (d *testDesktopViews) State() domain.DesktopNativeState {
	return domain.DesktopNativeState{LyricsLocked: true}
}
func (d *testDesktopViews) ShowView(s string) error                { d.kind = s; return nil }
func (d *testDesktopViews) HideView(s string) error                { d.kind = s; return nil }
func (d *testDesktopViews) Dispatch(c domain.DesktopCommand) error { d.command = c; return nil }
func (d *testDesktopViews) ResetPositions() error                  { d.reset = true; return nil }
func TestDesktopViewsDelegatesAndUnavailable(t *testing.T) {
	s := New(nil, nil)
	if _, e := s.GetDesktopEpoch(); e == nil {
		t.Fatal("unavailable")
	}
	d := &testDesktopViews{}
	s = New(nil, nil, Options{DesktopViews: func() DesktopViewActions { return d }})
	if epoch, e := s.GetDesktopEpoch(); e != nil || epoch != 7 {
		t.Fatal(epoch, e)
	}
	if snap, e := s.GetDesktopSnapshot(); e != nil || snap.Epoch != 7 {
		t.Fatal(e)
	}
	if native, e := s.GetDesktopNativeState(); e != nil || !native.LyricsLocked {
		t.Fatal(e)
	}
	_ = s.ShowDesktopView("mini")
	if d.kind != "mini" {
		t.Fatal(d.kind)
	}
	_ = s.HideDesktopView("lyrics")
	if d.kind != "lyrics" {
		t.Fatal(d.kind)
	}
	c := domain.DesktopCommand{Epoch: 7, Command: "toggle-play"}
	if e := s.DispatchDesktopCommand(c); e != nil || d.command != c {
		t.Fatal(e)
	}
	if e := s.DispatchDesktopCommand(domain.DesktopCommand{Epoch: 7, Command: "other"}); e == nil {
		t.Fatal("invalid delegated")
	}
	_ = s.ResetDesktopViewPositions()
	if !d.reset {
		t.Fatal("reset not delegated")
	}
}
