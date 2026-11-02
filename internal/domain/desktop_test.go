package domain

import (
	"math"
	"strings"
	"testing"
)

func TestDesktopSettingsDefaultsAndValidation(t *testing.T) {
	s := DefaultDesktopSettings()
	if NormalizeDesktopSettings(DesktopSettings{}) != s || ValidateDesktopSettings(s) != nil {
		t.Fatal("defaults")
	}
	for _, mutate := range []func(*DesktopSettings){func(s *DesktopSettings) { s.LyricsOpacity = math.NaN() }, func(s *DesktopSettings) { s.LyricsFontSize = 65 }, func(s *DesktopSettings) { s.MiniPlacement.Width = 0 }} {
		bad := s
		mutate(&bad)
		if ValidateDesktopSettings(bad) == nil {
			t.Fatal("accepted invalid settings")
		}
	}
	s.MiniPlacement = WindowPlacement{Valid: true, X: -1920, Y: -200, Width: 420, Height: 176}
	if ValidateDesktopSettings(s) != nil {
		t.Fatal("negative monitor origin rejected")
	}
}
func TestDesktopSnapshotBoundaries(t *testing.T) {
	s := DesktopSnapshot{Epoch: 1, Sequence: 1, Playback: "paused", Options: DefaultDesktopSettings()}
	if ValidateDesktopSnapshot(s) != nil {
		t.Fatal("valid empty snapshot")
	}
	for _, mutate := range []func(*DesktopSnapshot){func(s *DesktopSnapshot) { s.Title = strings.Repeat("a", 1025) }, func(s *DesktopSnapshot) { s.CurrentLyric = strings.Repeat("a", 4097) }, func(s *DesktopSnapshot) { s.CoverRef = "https://m1.music.126.net/song.mp3?token=secret" }, func(s *DesktopSnapshot) { s.Volume = math.Inf(1) }, func(s *DesktopSnapshot) { s.Sequence = 9007199254740992 }, func(s *DesktopSnapshot) { s.Provider = "other" }} {
		bad := s
		mutate(&bad)
		if ValidateDesktopSnapshot(bad) == nil {
			t.Fatal("accepted invalid projection")
		}
	}
}
