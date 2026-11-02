package appservice

import (
	"github.com/voici5986/otoneigo/internal/domain"
	"sync"
	"testing"
	"time"
)

func TestSettingsNotificationCanReenterAndKeepsNewestCommit(t *testing.T) {
	l := &qualityLibrary{settings: domain.DefaultSettings()}
	s := New(nil, nil, Options{Library: l})
	var seen []float64
	s.options.SettingsChanged = func(v domain.LocalSettings) {
		seen = append(seen, v.Volume)
		if v.Volume == .4 {
			next, e := s.GetSettings()
			if e != nil {
				t.Fatal(e)
			}
			next.Volume = .6
			if _, e = s.SetSettings(next); e != nil {
				t.Fatal(e)
			}
		}
	}
	v := domain.DefaultSettings()
	v.Volume = .4
	if _, e := s.SetSettings(v); e != nil {
		t.Fatal(e)
	}
	if len(seen) != 2 || seen[0] != .4 || seen[1] != .6 || l.settings.Volume != .6 {
		t.Fatal("notifications lost or reordered", seen, l.settings)
	}
}

func TestSettingsSlowNotificationDoesNotBlockNewCommit(t *testing.T) {
	s, db := openLibraryService(t, domain.Capabilities{})
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var seen []float64
	s.options.SettingsChanged = func(v domain.LocalSettings) {
		mu.Lock()
		seen = append(seen, v.Volume)
		mu.Unlock()
		if v.Volume == .2 {
			close(entered)
			<-release
		}
	}
	go func() { defer close(done); v := domain.DefaultSettings(); v.Volume = .2; _, _ = s.SetSettings(v) }()
	<-entered
	second := make(chan error, 1)
	go func() { v := domain.DefaultSettings(); v.Volume = .8; _, e := s.SetSettings(v); second <- e }()
	select {
	case e := <-second:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("notification held the commit lock")
	}
	if v, e := db.GetSettings(); e != nil || v.Volume != .8 {
		t.Fatal("latest commit missing", v, e)
	}
	close(release)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != .2 || seen[1] != .8 {
		t.Fatal("older callback replaced latest preferences", seen)
	}
}

func TestSettingsReadIsPureAndPreservesDormantPreference(t *testing.T) {
	for _, q := range []string{"standard", "lossless", "hires"} {
		t.Run(q, func(t *testing.T) {
			original := domain.DefaultSettings()
			original.PreferredQuality = q
			l := &qualityLibrary{settings: original}
			calls := 0
			s := New(nil, nil, Options{Library: l, SettingsChanged: func(domain.LocalSettings) { calls++ }})
			for range 3 {
				got, e := s.GetSettings()
				if e != nil || got.PreferredQuality != "exhigh" {
					t.Fatal(got, e)
				}
			}
			if l.writes != 0 || calls != 0 || l.settings != original {
				t.Fatalf("read mutated storage: writes=%d callbacks=%d quality=%s", l.writes, calls, l.settings.PreferredQuality)
			}
		})
	}
}

func TestAnonymousUnrelatedSettingsSavePreservesPremiumPreference(t *testing.T) {
	original := domain.DefaultSettings()
	original.PreferredQuality = "hires"
	l := &qualityLibrary{settings: original}
	s := New(nil, nil, Options{Library: l})
	visible, e := s.GetSettings()
	if e != nil {
		t.Fatal(e)
	}
	visible.Volume = .31
	got, e := s.SetSettings(visible)
	if e != nil || got.PreferredQuality != "exhigh" {
		t.Fatal(got, e)
	}
	if l.settings.PreferredQuality != "hires" || l.settings.Volume != .31 {
		t.Fatal("anonymous save destroyed configured preference", l.settings)
	}
	s.options.PlaybackEntitlement = func() domain.PlaybackEntitlement { return domain.PlaybackEntitlement{LoggedIn: true, Member: true} }
	restored, e := s.GetSettings()
	if e != nil || restored.PreferredQuality != "hires" {
		t.Fatal(restored, e)
	}
	restored.PreferredQuality = "exhigh"
	if _, e = s.SetSettings(restored); e != nil || l.settings.PreferredQuality != "exhigh" {
		t.Fatal("verified explicit preference not saved", e)
	}
}

func TestSettingsCallbackRunsOutsideCommitLock(t *testing.T) {
	for _, placement := range []bool{false, true} {
		l := &qualityLibrary{settings: domain.DefaultSettings()}
		s := New(nil, nil, Options{Library: l})
		blocked := false
		s.options.SettingsChanged = func(domain.LocalSettings) {
			done := make(chan struct{})
			go func() { _, _ = s.GetSettings(); close(done) }()
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
				blocked = true
			}
		}
		if placement {
			if e := s.SaveDesktopPlacement("mini", domain.WindowPlacement{Valid: true, X: 10, Y: 20, Width: 420, Height: 176}); e != nil {
				t.Fatal(e)
			}
		} else {
			v := domain.DefaultSettings()
			v.Volume = .4
			if _, e := s.SetSettings(v); e != nil {
				t.Fatal(e)
			}
		}
		if blocked {
			t.Fatal("callback blocked reading settings behind commit lock")
		}
	}
}
