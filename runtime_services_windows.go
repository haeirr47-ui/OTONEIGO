package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/voici5986/otoneigo/internal/catalog"
	"github.com/voici5986/otoneigo/internal/diagnostics"
	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/gd"
	"github.com/voici5986/otoneigo/internal/mediacache"
	"github.com/voici5986/otoneigo/internal/region"
	"github.com/voici5986/otoneigo/internal/resourcecache"
	"github.com/voici5986/otoneigo/internal/stream"
)

type applicationCatalog struct {
	*catalog.Client
	resolver   *stream.Resolver
	audioCache playbackCache
	resources  *resourcecache.Manager
}

type playbackCache interface {
	Prepare(context.Context, domain.ResolvedStream) (domain.ResolvedStream, error)
}

func (c applicationCatalog) Lyrics(ctx context.Context, id string) (domain.LyricData, error) {
	if c.resources == nil {
		return c.Client.Lyrics(ctx, id)
	}
	return c.resources.Lyrics(ctx, id, c.Client.Lyrics)
}

func (c applicationCatalog) Resolve(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	if c.audioCache == nil {
		return c.resolver.Resolve(ctx, id, quality)
	}
	v, err := c.resolver.ResolvePrepared(ctx, id, quality, func(stage context.Context, v domain.ResolvedStream) (domain.ResolvedStream, error) {
		prepared, err := c.audioCache.Prepare(stage, v)
		if err != nil && mediacache.IsCacheFailure(err) && !v.CacheHit && stage.Err() == nil && safeDirectPlayback(v, id) {
			return v, nil
		}
		return prepared, err
	}, mediacache.IsCacheFailure)
	if err == nil && v.URL != "" && v.CacheToken == "" && !trustedProviderAddress(v, id) {
		return domain.ResolvedStream{}, errors.New("UNTRUSTED_MEDIA_URL")
	}
	return v, err
}

func safeDirectPlayback(v domain.ResolvedStream, id string) bool {
	return v.Status == "full" && trustedProviderAddress(v, id)
}

func trustedProviderAddress(v domain.ResolvedStream, id string) bool {
	if v.CacheHit || v.SongID != id || (v.Provider != "netease-direct" && v.Provider != "gd-netease") || len(v.URL) > 16384 {
		return false
	}
	u, err := url.Parse(v.URL)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && u.Path != "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".music.126.net")
}

type observedCatalog struct {
	*catalog.Client
	region *region.Controller
	log    *diagnostics.Recorder
}

func diagnosticCode(err error, stream domain.ResolvedStream) string {
	if errors.Is(err, context.Canceled) {
		return "CANCELLED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT"
	}
	if err != nil {
		return err.Error()
	}
	return stream.ErrorCode
}

func (c observedCatalog) record(stage, id, quality string, start time.Time, v domain.ResolvedStream, err error) {
	status := c.region.Status()
	c.log.Record(diagnostics.Event{Stage: stage, SongID: id, Country: status.Country, Strategy: v.AccessStrategy, Provider: v.Provider, Status: v.Status, RequestedQuality: quality, ActualQuality: v.ActualQuality, ActualFormat: v.ActualFormat, ActualBitrate: v.ActualBitrate, ErrorCode: diagnosticCode(err, v), ElapsedMs: time.Since(start).Milliseconds()})
}

func (c observedCatalog) Resolve(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	start := time.Now()
	v, err := c.Client.Resolve(ctx, id, quality)
	c.record("normal-api", id, quality, start, v, err)
	return v, err
}
func (c observedCatalog) ResolveCompatible(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	start := time.Now()
	v, err := c.Client.ResolveCompatible(ctx, id, quality)
	c.record("compatible-api", id, quality, start, v, err)
	return v, err
}

type observedFallback struct {
	*gd.Client
	region *region.Controller
	log    *diagnostics.Recorder
}

func (c observedFallback) Resolve(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	start := time.Now()
	v, err := c.Client.Resolve(ctx, id, quality)
	c.log.Record(diagnostics.Event{Stage: "gd-api", SongID: id, Country: c.region.Status().Country, Strategy: v.AccessStrategy, Provider: v.Provider, Status: v.Status, RequestedQuality: quality, ActualQuality: v.ActualQuality, ActualFormat: v.ActualFormat, ActualBitrate: v.ActualBitrate, ErrorCode: diagnosticCode(err, v), ElapsedMs: time.Since(start).Milliseconds()})
	return v, err
}
