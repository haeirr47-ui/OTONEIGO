//go:build windows

package main

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/mediacache"
	"github.com/voici5986/otoneigo/internal/stream"
	"strings"
	"testing"
	"time"
)

type retryCatalog struct {
	calls int
	value domain.ResolvedStream
}

func (c *retryCatalog) Resolve(context.Context, string, string) (domain.ResolvedStream, error) {
	c.calls++
	return c.value, nil
}
func (c *retryCatalog) ResolveCompatible(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
	return c.Resolve(ctx, id, q)
}

type retryPreparer struct {
	manager *mediacache.Manager
	calls   int
	before  func()
}

func (p *retryPreparer) Prepare(ctx context.Context, v domain.ResolvedStream) (domain.ResolvedStream, error) {
	p.calls++
	if p.before != nil {
		p.before()
	}
	return p.manager.Prepare(ctx, v)
}
func TestRemovedCachedCandidateRetriesNetworkOnceWithinOriginalScope(t *testing.T) {
	for _, mode := range []string{"retry", "cancel", "invalidate", "untrusted"} {
		t.Run(mode, func(t *testing.T) {
			manager, e := mediacache.New(mediacache.Options{Root: t.TempDir()})
			if e != nil {
				t.Fatal(e)
			}
			defer manager.Close()
			direct := domain.ResolvedStream{SongID: "123", Provider: "netease-direct", Status: "full", URL: "https://m1.music.126.net/full.mp3", ActualQuality: "exhigh", ActualFormat: "mp3", ActualBitrate: 320000, DurationMs: 120000}
			provider := &retryCatalog{value: direct}
			if mode == "untrusted" {
				provider.value.URL = "http://127.0.0.1/private"
			}
			lookups := 0
			resolver := stream.NewResolver(provider, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off"} }, stream.Options{CachedStream: func(context.Context, string, string, domain.PlaybackEntitlement) (domain.ResolvedStream, bool) {
				lookups++
				v := direct
				v.CacheHit = true
				v.URL = "cache:" + strings.Repeat("a", 64)
				v.CacheEntryID = strings.Repeat("a", 64)
				return v, true
			}})
			defer resolver.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			p := &retryPreparer{manager: manager, before: func() {
				_, _ = manager.Clear()
				if mode == "cancel" {
					cancel()
				}
				if mode == "invalidate" {
					resolver.Invalidate()
				}
			}}
			c := applicationCatalog{resolver: resolver, audioCache: p}
			got, err := c.Resolve(ctx, "123", "exhigh")
			if mode == "cancel" || mode == "invalidate" {
				if !errors.Is(err, context.Canceled) || provider.calls != 0 {
					t.Fatal("cancelled admission retried", err, provider.calls)
				}
				return
			}
			if mode == "untrusted" {
				if err == nil || err.Error() != "UNTRUSTED_MEDIA_URL" {
					t.Fatal("fresh retry bypassed URL validation", err)
				}
				return
			}
			deadline, _ := ctx.Deadline()
			if err != nil || got.URL != direct.URL || got.CacheHit || provider.calls != 1 || p.calls != 1 || lookups != 1 || got.ResolutionDeadlineAt > deadline.UnixMilli() {
				t.Fatalf("cache admission failure not recovered: err=%v net=%d prep=%d lookup=%d cache=%v", err, provider.calls, p.calls, lookups, got.CacheHit)
			}
		})
	}
}
func TestCacheUnavailableDoesNotDiscardValidatedDirectAddress(t *testing.T) {
	manager, e := mediacache.New(mediacache.Options{Root: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	manager.Close()
	for _, url := range []string{"https://m1.music.126.net/full.mp3", "http://127.0.0.1/private"} {
		v := domain.ResolvedStream{SongID: "123", Provider: "netease-direct", Status: "full", URL: url, ActualQuality: "exhigh", ActualFormat: "mp3", ActualBitrate: 320000, DurationMs: 120000}
		r := stream.NewResolver(&retryCatalog{value: v}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off"} })
		got, err := (applicationCatalog{resolver: r, audioCache: manager}).Resolve(context.Background(), "123", "exhigh")
		r.Close()
		if strings.HasPrefix(url, "https:") {
			if err != nil || got.URL != url {
				t.Fatal("validated network URL discarded", err)
			}
		} else if err == nil {
			t.Fatal("cache failure bypassed URL trust")
		}
	}
}

func TestPreparedUnknownCompatibilityKeepsExistingPlaybackSemantics(t *testing.T) {
	m, err := mediacache.New(mediacache.Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	v := domain.ResolvedStream{SongID: "123", Provider: "netease-direct", Status: "unknown", URL: "https://m1.music.126.net/unknown.mp3", ErrorCode: ""}
	r := stream.NewResolver(&retryCatalog{value: v}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "always"} })
	defer r.Close()
	got, err := (applicationCatalog{resolver: r, audioCache: m}).Resolve(context.Background(), "123", "exhigh")
	if err != nil || got.Status != "unknown" || got.URL != v.URL {
		t.Fatal("known trusted compatibility result became trust error", got.Status, err)
	}
}
