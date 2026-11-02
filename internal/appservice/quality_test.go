package appservice

import (
	"context"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

type qualityCatalog struct {
	lateCatalog
	quality string
	calls   int
	budget  time.Duration
}

func (c *qualityCatalog) Resolve(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	c.quality = quality
	c.calls++
	deadline, _ := ctx.Deadline()
	c.budget = time.Until(deadline)
	return domain.ResolvedStream{}, nil
}

func TestResolveStreamEnforcesBackendEntitlement(t *testing.T) {
	for _, e := range []domain.PlaybackEntitlement{{}, {LoggedIn: true}, {Member: true}, {LoggedIn: true, Member: true}} {
		catalog := &qualityCatalog{}
		s := New(catalog, nil, Options{PlaybackEntitlement: func() domain.PlaybackEntitlement { return e }})
		for _, q := range []string{"standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"} {
			if _, err := s.ResolveStream(context.Background(), "123", q); err != nil {
				t.Fatal(err)
			}
			want := domain.NormalizePreferredQuality(q, e)
			if catalog.quality != want {
				t.Fatalf("frontend %s bypassed %+v: %s", q, e, catalog.quality)
			}
		}
		calls := catalog.calls
		if _, err := s.ResolveStream(context.Background(), "123", "premium=true"); err == nil || err.Error() != "INVALID_QUALITY" || catalog.calls != calls {
			t.Fatal("invalid request reached catalog", err)
		}
		if catalog.budget > domain.ResolveQualityTimeout(e) || catalog.budget < domain.ResolveQualityTimeout(e)-time.Second {
			t.Fatal("wrong total deadline", catalog.budget)
		}
		if s.GetPlaybackQualityPolicy().PremiumAllowed != (e.LoggedIn && e.Member) {
			t.Fatal("policy mismatch")
		}
	}
	if New(nil, nil).GetPlaybackQualityPolicy().PremiumAllowed {
		t.Fatal("nil provider enabled premium")
	}
	catalog := &qualityCatalog{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := New(catalog, nil).ResolveStream(ctx, "123", "exhigh"); err != nil || catalog.budget > time.Second {
		t.Fatal("caller deadline was extended", err, catalog.budget)
	}
}

type qualityLibrary struct {
	LocalLibrary
	settings domain.LocalSettings
	writes   int
}

func (l *qualityLibrary) GetSettings() (domain.LocalSettings, error) { return l.settings, nil }
func (l *qualityLibrary) SetSettings(s domain.LocalSettings) error {
	l.settings = s
	l.writes++
	return nil
}

func TestSetSettingsRejectsFallbackAndUnverifiedPremium(t *testing.T) {
	for _, e := range []domain.PlaybackEntitlement{{}, {Member: true}, {LoggedIn: true}, {LoggedIn: true, Member: true}} {
		for _, q := range []string{"standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"} {
			l := &qualityLibrary{settings: domain.DefaultSettings()}
			calls := 0
			s := New(nil, nil, Options{Library: l, Capabilities: domain.Capabilities{GD: true}, PlaybackEntitlement: func() domain.PlaybackEntitlement { return e }, SettingsChanged: func(domain.LocalSettings) { calls++ }})
			next := domain.LocalSettings{PreferredQuality: q, RegionMode: "always", GDEnabled: true, Volume: .3, AudioCache: domain.DefaultAudioCacheSettings(), ResourceCache: domain.DefaultResourceCacheSettings(), Desktop: domain.DefaultDesktopSettings()}
			_, err := s.SetSettings(next)
			want := ""
			if q == "standard" || q == "higher" {
				want = "QUALITY_FALLBACK_ONLY"
			} else if q != "exhigh" && !(e.LoggedIn && e.Member) {
				want = "QUALITY_MEMBERSHIP_REQUIRED"
			}
			if want != "" {
				if err == nil || err.Error() != want || l.writes != 0 || calls != 0 {
					t.Fatalf("%s/%+v: %v writes%d calls%d", q, e, err, l.writes, calls)
				}
			} else if err != nil || l.settings != next || l.writes != 1 || calls != 1 {
				t.Fatalf("valid %s/%+v: %v", q, e, err)
			}
			if want == "" {
				if got, err := s.GetSettings(); err != nil || got != next || l.writes != 1 {
					t.Fatalf("allowed preference changed on read: %+v %v", got, err)
				}
			}
		}
	}
}

// The library only implements settings: any accidental queue, history, favorite
// or other metadata operation would panic through the embedded nil interface.
func TestGetSettingsNormalizesViewWithoutPersisting(t *testing.T) {
	for _, q := range []string{"standard", "higher", "lossless", "hires", "jyeffect", "sky", "jymaster", "unknown"} {
		l := &qualityLibrary{settings: domain.LocalSettings{PreferredQuality: q, RegionMode: "always", GDEnabled: true, Volume: .37, AudioCache: domain.DefaultAudioCacheSettings(), ResourceCache: domain.DefaultResourceCacheSettings(), Desktop: domain.DefaultDesktopSettings()}}
		want := l.settings
		want.PreferredQuality = "exhigh"
		calls := 0
		s := New(nil, nil, Options{Library: l, SettingsChanged: func(got domain.LocalSettings) {
			calls++
		}})
		for i := 0; i < 2; i++ {
			got, err := s.GetSettings()
			if err != nil || got != want {
				t.Fatalf("%s: %+v %v", q, got, err)
			}
		}
		if l.writes != 0 || calls != 0 || l.settings.PreferredQuality != q {
			t.Fatalf("%s writes%d calls%d", q, l.writes, calls)
		}
	}
}
