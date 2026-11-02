package appservice

import (
	"errors"
	"sync"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestStaleSetSettingsPreservesNativeDesktopGeometry(t *testing.T) {
	s, db := openLibraryService(t, domain.Capabilities{})
	stale, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	mini := domain.WindowPlacement{Valid: true, X: -1920, Y: 50, Width: 480, Height: 200}
	lyrics := domain.WindowPlacement{Valid: true, X: 100, Y: -200, Width: 900, Height: 180}
	if err = s.SaveDesktopPlacement("mini", mini); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDesktopPlacement("lyrics", lyrics); err != nil {
		t.Fatal(err)
	}
	stale.Volume = .42
	stale.Desktop.LyricsFontSize = 36
	stale.Desktop.LyricsLocked = true
	want := stale
	want.Desktop.MiniPlacement, want.Desktop.LyricsPlacement = mini, lyrics
	calls := 0
	s.options.SettingsChanged = func(got domain.LocalSettings) {
		calls++
		persisted, e := db.GetSettings()
		if e != nil || persisted != want || got != want {
			t.Errorf("callback before merged commit: %+v %+v %v", got, persisted, e)
		}
	}
	if got, e := s.SetSettings(stale); e != nil || got != want {
		t.Fatalf("stale UI overwrote placement: %+v %v", got, e)
	}
	if calls != 1 {
		t.Fatal("notification count", calls)
	}
	// A renderer without the desktop section preserves all native options as well.
	legacy := want
	legacy.Desktop = domain.DesktopSettings{}
	if got, e := s.SetSettings(legacy); e != nil || got != want {
		t.Fatalf("legacy UI reset desktop options: %+v %v", got, e)
	}
}

func TestConcurrentDesktopPlacementAndSettingsMerge(t *testing.T) {
	s, db := openLibraryService(t, domain.Capabilities{})
	stale, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	stale.Desktop.LyricsOpacity = .6
	stale.CloseToTray, stale.Volume = true, .25
	mini := domain.WindowPlacement{Valid: true, X: -900, Y: 30, Width: 500, Height: 220}
	lyrics := domain.WindowPlacement{Valid: true, X: 10, Y: 700, Width: 950, Height: 190}
	start := make(chan struct{})
	results := make(chan error, 3)
	var wg sync.WaitGroup
	for _, action := range []func() error{
		func() error { return s.SaveDesktopPlacement("mini", mini) },
		func() error { return s.SaveDesktopPlacement("lyrics", lyrics) },
		func() error { _, e := s.SetSettings(stale); return e },
	} {
		wg.Add(1)
		go func(action func() error) { defer wg.Done(); <-start; results <- action() }(action)
	}
	close(start)
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	want := stale
	want.Desktop.MiniPlacement, want.Desktop.LyricsPlacement = mini, lyrics
	if got, e := db.GetSettings(); e != nil || got != want {
		t.Fatalf("concurrent update lost: %+v %v", got, e)
	}
}

type placementFailureLibrary struct {
	LocalLibrary
	readError, writeError error
	writes                int
}

func (l *placementFailureLibrary) GetSettings() (domain.LocalSettings, error) {
	return domain.DefaultSettings(), l.readError
}
func (l *placementFailureLibrary) SetSettings(domain.LocalSettings) error {
	l.writes++
	return l.writeError
}

func TestDesktopPlacementValidationAndSanitizedFailures(t *testing.T) {
	valid := domain.DefaultDesktopSettings().MiniPlacement
	secret := errors.New(`C:\private\token=secret`)
	for _, tc := range []struct {
		name, kind string
		placement  domain.WindowPlacement
		library    LocalLibrary
		want       string
	}{
		{"invalid view", "main", valid, nil, "INVALID_DESKTOP_VIEW"},
		{"invalid placement", "mini", domain.WindowPlacement{}, nil, "INVALID_DESKTOP_PLACEMENT"},
		{"missing storage", "lyrics", valid, nil, "LOCAL_STORAGE_UNAVAILABLE"},
		{"read failure", "mini", valid, &placementFailureLibrary{readError: secret}, "LOCAL_STORAGE_UNAVAILABLE"},
		{"write failure", "lyrics", valid, &placementFailureLibrary{writeError: secret}, "LOCAL_STORAGE_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := New(nil, nil, Options{Library: tc.library, SettingsChanged: func(domain.LocalSettings) { calls++ }})
			if err := s.SaveDesktopPlacement(tc.kind, tc.placement); err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if calls != 0 {
				t.Fatal("failed write notified")
			}
			if l, ok := tc.library.(*placementFailureLibrary); ok && l.readError != nil && l.writes != 0 {
				t.Fatal("read failure wrote settings")
			}
		})
	}
}
