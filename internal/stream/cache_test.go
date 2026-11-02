package stream

import (
	"context"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
	"time"
)

func TestVerifiedCacheHitPrecedesEveryNetworkSource(t *testing.T) {
	for _, mode := range []string{"auto", "always", "off"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) { calls++; return domain.ResolvedStream{}, nil }, compatible: func(context.Context) (domain.ResolvedStream, error) {
				t.Fatal("cache hit entered compatibility")
				return domain.ResolvedStream{}, nil
			}}
			r := NewResolver(c, &fakeRegion{"overseas", make(chan struct{})}, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: mode, GDEnabled: true} }, Options{CachedStream: func(_ context.Context, id, q string, _ domain.PlaybackEntitlement) (domain.ResolvedStream, bool) {
				if id != "123" {
					return domain.ResolvedStream{}, false
				}
				return domain.ResolvedStream{SongID: id, Provider: "gd-netease", Status: "full", URL: "cache:verified", CacheHit: true, CacheEntryID: "verified", ActualQuality: q, ActualBitrate: 320000, ActualFormat: "mp3"}, true
			}})
			defer r.Close()
			f := &cacheFallback{}
			r.SetFallback(f)
			// Model a previously cooled selected path without bypassing cache lookup.
			r.cooldownNetwork = r.region.Changes()
			r.coolUntil = time.Now().Add(time.Minute)
			coolUntil := r.coolUntil
			r.failures = 3
			v, err := r.Resolve(context.Background(), "123", "exhigh")
			if err != nil || !v.CacheHit || v.Provider != "gd-netease" || calls != 0 || f.calls != 0 || v.ResolutionDeadlineAt == 0 {
				t.Fatal("cache did not satisfy source without network", v, err, calls)
			}
			if !r.coolUntil.Equal(coolUntil) || r.failures != 3 {
				t.Fatal("cached media reset compatibility health", r.coolUntil, r.failures)
			}
			if mode != "off" {
				// A different uncached song must retain the cooled selected path,
				// while enabled GD may still supply it.
				f.resolve = func(context.Context) (domain.ResolvedStream, error) {
					return domain.ResolvedStream{SongID: "456", Provider: "gd-netease", Status: "full", URL: "https://m7.music.126.net/gd.mp3"}, nil
				}
				v, err = r.Resolve(context.Background(), "456", "exhigh")
				if err != nil || v.Provider != "gd-netease" || v.CacheHit || calls != 0 || c.calls != 0 || f.calls != 1 || !r.coolUntil.Equal(coolUntil) || r.failures != 3 {
					t.Fatal("cache hit bypassed cooldown on next request", v, err, calls, c.calls, f.calls)
				}
			}
		})
	}
}

func TestLowerCacheIsOnlyUsedAfterHigherQualityMisses(t *testing.T) {
	for _, source := range []string{"normal", "compatible"} {
		t.Run(source, func(t *testing.T) {
			var queries []string
			c := qualityCatalog{func(_ context.Context, strategy, _ string) (domain.ResolvedStream, error) {
				if strategy != source {
					t.Error("cache lookup switched playback path", strategy)
				}
				return domain.ResolvedStream{Status: "unavailable"}, nil
			}}
			mode := "off"
			if source == "compatible" {
				mode = "always"
			}
			r := NewResolver(c, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: mode} }, Options{CachedStream: func(_ context.Context, id, q string, _ domain.PlaybackEntitlement) (domain.ResolvedStream, bool) {
				queries = append(queries, q)
				if q != "standard" {
					return domain.ResolvedStream{}, false
				}
				return domain.ResolvedStream{SongID: id, Provider: "netease-direct", Status: "full", URL: "cache:lower", CacheHit: true, CacheEntryID: "lower", ActualQuality: q, ActualBitrate: 128000, ActualFormat: "mp3"}, true
			}})
			defer r.Close()
			v, err := r.Resolve(context.Background(), "123", "exhigh")
			if err != nil || !v.CacheHit || v.ActualQuality != "standard" || v.RequestedQuality != "exhigh" || len(queries) != 3 || queries[0] != "exhigh" || queries[1] != "higher" || queries[2] != "standard" {
				t.Fatal("cache skipped quality preference", queries, v, err)
			}
		})
	}
}
