package stream

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

type Catalog interface {
	Resolve(context.Context, string, string) (domain.ResolvedStream, error)
	ResolveCompatible(context.Context, string, string) (domain.ResolvedStream, error)
}
type Region interface {
	Status() domain.RegionStatus
	Changes() <-chan struct{}
}
type Fallback interface {
	Resolve(context.Context, string, string) (domain.ResolvedStream, error)
}
type Options struct {
	PlaybackEntitlement func() domain.PlaybackEntitlement
	CachedStream        func(context.Context, string, string, domain.PlaybackEntitlement) (domain.ResolvedStream, bool)
}

type Resolver struct {
	cachedStream    func(context.Context, string, string, domain.PlaybackEntitlement) (domain.ResolvedStream, bool)
	entitlement     func() domain.PlaybackEntitlement
	catalog         Catalog
	region          Region
	settings        func() domain.LocalSettings
	mu              sync.Mutex
	invalidated     chan struct{}
	closed          bool
	fallback        Fallback
	fallbackReady   <-chan struct{}
	failures        int
	coolUntil       time.Time
	cooldownNetwork <-chan struct{}
}

func NewResolver(c Catalog, r Region, settings func() domain.LocalSettings, options ...Options) *Resolver {
	if settings == nil {
		settings = domain.DefaultSettings
	}
	ready := make(chan struct{})
	close(ready)
	var entitlement func() domain.PlaybackEntitlement
	var cached func(context.Context, string, string, domain.PlaybackEntitlement) (domain.ResolvedStream, bool)
	if len(options) > 0 {
		entitlement = options[0].PlaybackEntitlement
		cached = options[0].CachedStream
	}
	return &Resolver{cachedStream: cached, entitlement: entitlement, catalog: c, region: r, settings: settings, invalidated: make(chan struct{}), fallbackReady: ready}
}

func invalidateFallback(f Fallback) {
	if invalidator, ok := f.(interface{ Invalidate() }); ok {
		invalidator.Invalidate()
	}
}

// SetFallback is an explicit capability gate. Production leaves it nil until verified.
func (r *Resolver) SetFallback(f Fallback) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	old := r.fallback
	r.fallback = f
	close(r.invalidated)
	r.invalidated = make(chan struct{})
	r.failures = 0
	r.coolUntil = time.Time{}
	ready := make(chan struct{})
	r.fallbackReady = ready
	r.mu.Unlock()
	invalidateFallback(old)
	invalidateFallback(f)
	close(ready)
}
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	close(r.invalidated)
	r.invalidated = make(chan struct{})
	r.failures = 0
	r.coolUntil = time.Time{}
	fallback := r.fallback
	ready := make(chan struct{})
	r.fallbackReady = ready
	r.mu.Unlock()
	invalidateFallback(fallback)
	close(ready)
}
func (r *Resolver) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		close(r.invalidated)
	}
}

type reply struct {
	stream domain.ResolvedStream
	err    error
}

// phase protects callers even when an injected provider ignores context cancellation.
func phase(ctx context.Context, d time.Duration, call func(context.Context) (domain.ResolvedStream, error)) (domain.ResolvedStream, error) {
	if err := ctx.Err(); err != nil {
		return domain.ResolvedStream{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	ch := make(chan reply, 1)
	go func() { v, e := call(ctx); ch <- reply{v, e} }()
	select {
	case <-ctx.Done():
		return domain.ResolvedStream{}, ctx.Err()
	case v := <-ch:
		if ctx.Err() != nil {
			return domain.ResolvedStream{}, ctx.Err()
		}
		return v.stream, v.err
	}
}

// Entitlement results stop regional header rewriting, but an explicitly enabled
// independent same-ID fallback may still provide a verified full stream.
func noCompatibility(v domain.ResolvedStream, e error) bool {
	return noFallback(v, e) || v.Status == "trial" || v.Status == "unauthorized" || v.ErrorCode == "LOGIN_REQUIRED" || (e != nil && e.Error() == "LOGIN_REQUIRED")
}
func noFallback(v domain.ResolvedStream, e error) bool {
	if errors.Is(e, context.Canceled) {
		return true
	}
	unsafe := func(code string) bool {
		switch code {
		case "GD_RATE_LIMITED", "GD_BUDGET_EXHAUSTED", "RATE_LIMITED", "INVALID_SONG_ID", "INVALID_QUALITY", "MISMATCHED_SONG_ID", "UNTRUSTED_MEDIA_URL", "INVALID_PROVIDER_RESPONSE":
			return true
		}
		return false
	}
	return unsafe(v.ErrorCode) || (e != nil && unsafe(e.Error()))
}

// enforceQuality rejects explicit premium evidence without altering honest actual fields.
func enforceQuality(v domain.ResolvedStream, policy domain.PlaybackQualityPolicy) domain.ResolvedStream {
	if policy.PremiumAllowed {
		return v
	}
	premium := v.ActualBitrate > policy.MaxBitrate
	switch strings.ToLower(v.ActualQuality) {
	case "lossless", "hires", "jyeffect", "sky", "jymaster":
		premium = true
	}
	switch strings.ToLower(v.ActualFormat) {
	case "flac", "alac":
		premium = true
	}
	if premium {
		v.URL = ""
		if !noCompatibility(v, nil) {
			v.Status = "unavailable"
			v.ErrorCode = "QUALITY_NOT_ALLOWED"
		}
	}
	return v
}

// A stage shares one budget across its quality ladder, preserving source priority.
func qualityStage(ctx context.Context, budget time.Duration, qualities []string, policy domain.PlaybackQualityPolicy, compatible, gd bool, id string, call func(context.Context, string, string) (domain.ResolvedStream, error), cache ...func(context.Context, string, string) (domain.ResolvedStream, bool)) (domain.ResolvedStream, error) {
	var protectedMu sync.Mutex
	var protected domain.ResolvedStream
	var protectedErr error
	observedProtected := false
	out, outErr := phase(ctx, budget, func(stage context.Context) (domain.ResolvedStream, error) {
		var first domain.ResolvedStream
		var firstErr error
		seenGD := map[string]bool{}
		for i, q := range qualities {
			if err := stage.Err(); err != nil {
				return first, err
			}
			if gd {
				key := q
				switch q {
				case "hires", "jyeffect", "sky", "jymaster":
					key = "999"
				}
				if seenGD[key] {
					continue
				}
				seenGD[key] = true
			}
			var v domain.ResolvedStream
			var err error
			cached := false
			if len(cache) > 0 && cache[0] != nil {
				v, cached = cache[0](stage, id, q)
			}
			if !cached {
				v, err = call(stage, id, q)
			}
			if stage.Err() != nil {
				return domain.ResolvedStream{}, stage.Err()
			}
			if gd && !cached && err == nil && v.URL != "" {
				if v.SongID != id {
					return domain.ResolvedStream{}, errors.New("MISMATCHED_SONG_ID")
				}
				if v.Provider != "gd-netease" {
					return domain.ResolvedStream{}, errors.New("INVALID_PROVIDER_RESPONSE")
				}
			}
			v = enforceQuality(v, policy)
			if i == 0 {
				first, firstErr = v, err
			}
			if noFallback(v, err) {
				return v, err
			}
			if !gd && noCompatibility(v, err) {
				protectedMu.Lock()
				if !observedProtected {
					protected, protectedErr, observedProtected = v, err, true
				}
				protectedMu.Unlock()
			}
			usable := err == nil && v.URL != "" && (v.Status == "full" || compatible && v.Status == "unknown")
			if gd && !cached {
				usable = usable && v.Status == "full" && v.SongID == id && v.Provider == "gd-netease"
			}
			if usable {
				v.RequestedQuality = qualities[0]
				return v, nil
			}
			if !gd && noCompatibility(v, err) && !noCompatibility(first, firstErr) {
				first, firstErr = v, err
			}
		}
		return first, firstErr
	})
	// A stage timeout cannot erase an entitlement cause already observed at this
	// source. The caller's cancellation or total deadline always takes precedence.
	if errors.Is(outErr, context.DeadlineExceeded) && ctx.Err() == nil {
		protectedMu.Lock()
		defer protectedMu.Unlock()
		if observedProtected {
			protected.URL = ""
			return protected, protectedErr
		}
	}
	return out, outErr
}

func (r *Resolver) Resolve(parent context.Context, id, quality string) (out domain.ResolvedStream, err error) {
	return r.resolve(parent, id, quality, nil, nil, false)
}

// ResolvePrepared keeps source selection and local admission under one deadline
// and policy/network generation. A disappearing cached candidate gets one fresh
// resolution, without another cache lookup or admission loop.
func (r *Resolver) ResolvePrepared(parent context.Context, id, quality string, prepare func(context.Context, domain.ResolvedStream) (domain.ResolvedStream, error), cacheFailure func(error) bool) (domain.ResolvedStream, error) {
	return r.resolve(parent, id, quality, prepare, cacheFailure, false)
}

func (r *Resolver) resolve(parent context.Context, id, quality string, prepare func(context.Context, domain.ResolvedStream) (domain.ResolvedStream, error), cacheFailure func(error) bool, skipCache bool) (out domain.ResolvedStream, err error) {
	r.mu.Lock()
	policyGeneration := r.invalidated
	r.mu.Unlock()
	entitlement := domain.PlaybackEntitlement{}
	if r.entitlement != nil {
		entitlement = r.entitlement()
	}
	quality, err = domain.EffectivePlaybackQuality(quality, entitlement)
	if err != nil {
		return out, err
	}
	if !domain.ValidID(id) {
		return out, errors.New("INVALID_SONG_ID")
	}
	policy := domain.QualityPolicy(entitlement)
	qualities := domain.QualityFallbacks(quality)
	ctx, cancel := context.WithTimeout(parent, domain.ResolveQualityTimeout(entitlement))
	defer cancel()
	deadline, _ := ctx.Deadline()
	var network <-chan struct{}
	classification := "unknown"
	if r.region != nil {
		network = r.region.Changes()
		classification = r.region.Status().Classification
	}
	r.mu.Lock()
	var networkReady chan struct{}
	if network != r.cooldownNetwork {
		networkReady = make(chan struct{})
		r.fallbackReady = networkReady
		r.cooldownNetwork = network
		r.failures = 0
		r.coolUntil = time.Time{}
	}
	invalid := r.invalidated
	closed := r.closed || invalid != policyGeneration
	fallback := r.fallback
	fallbackReady := r.fallbackReady
	cool := time.Now().Before(r.coolUntil)
	r.mu.Unlock()
	if networkReady != nil {
		go func() { invalidateFallback(fallback); close(networkReady) }()
	}
	settings := r.settings()
	if closed {
		return out, context.Canceled
	}
	watcherDone := make(chan struct{})
	defer close(watcherDone)
	go func() {
		select {
		case <-invalid:
			cancel()
		case <-network:
			cancel()
		case <-ctx.Done():
		case <-watcherDone:
		}
	}()
	activeErr := func() error {
		select {
		case <-invalid:
			cancel()
			return context.Canceled
		default:
		}
		select {
		case <-network:
			cancel()
			return context.Canceled
		default:
		}
		return ctx.Err()
	}
	// Recheck both generations before handing a result to the application; no URL cache exists.
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		select {
		case <-invalid:
			out = domain.ResolvedStream{}
			err = context.Canceled
			return
		default:
		}
		select {
		case <-network:
			out = domain.ResolvedStream{}
			err = context.Canceled
			return
		default:
		}
		if ctx.Err() != nil {
			out = domain.ResolvedStream{}
			err = ctx.Err()
		}
		if err == nil && out.URL != "" {
			out.ResolutionDeadlineAt = deadline.UnixMilli()
		}
	}()
	defer func() {
		if prepare == nil || err != nil || out.URL == "" {
			return
		}
		candidate := out
		out, err = prepare(ctx, candidate)
		if err == nil || !candidate.CacheHit || cacheFailure == nil || !cacheFailure(err) {
			return
		}
		if cancelled := activeErr(); cancelled != nil {
			out = domain.ResolvedStream{}
			err = cancelled
			return
		}
		out, err = r.resolve(ctx, id, quality, nil, nil, true)
	}()
	if e := activeErr(); e != nil {
		return out, e
	}
	lookup := func(c context.Context, id, q string) (domain.ResolvedStream, bool) {
		if skipCache || r.cachedStream == nil {
			return domain.ResolvedStream{}, false
		}
		return r.cachedStream(c, id, q, entitlement)
	}
	// Select the playback path once, before walking its quality ladder. Unknown
	// regions retain the conservative ordinary-then-compatible experiment.
	directCompatible := settings.RegionMode == "always" || (settings.RegionMode == "auto" && classification == "overseas")
	var normal domain.ResolvedStream
	var normalErr error
	if !directCompatible {
		normal, normalErr = qualityStage(ctx, 4*time.Second, qualities, policy, false, false, id, r.catalog.Resolve, lookup)
		if e := activeErr(); e != nil {
			return out, e
		}
		if normal.AccessStrategy == "" {
			normal.AccessStrategy = "normal"
		}
		if normalErr == nil && normal.Status == "full" && normal.URL != "" {
			return normal, nil
		}
		if noFallback(normal, normalErr) {
			return normal, normalErr
		}
	}
	authoritative, authoritativeErr := normal, normalErr
	eligible := directCompatible || (settings.RegionMode == "auto" && classification != "CN")
	if eligible && (directCompatible || !cool && !noCompatibility(normal, normalErr)) {
		compatibleCall := r.catalog.ResolveCompatible
		if cool {
			// A cooldown must never turn a selected overseas path into ordinary
			// requests. Still check verified media at each quality before GD.
			compatibleCall = func(context.Context, string, string) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unavailable", ErrorCode: "COMPATIBILITY_COOLDOWN"}, nil
			}
		}
		compatible, compatErr := qualityStage(ctx, 5*time.Second, qualities, policy, true, false, id, compatibleCall, lookup)
		if e := activeErr(); e != nil {
			return out, e
		}
		if !compatible.CacheHit {
			compatible.AccessStrategy = "region-header"
		}
		compatible.RegionSuspected = classification == "unknown"
		if noFallback(compatible, compatErr) {
			compatible.RegionSuspected = false
			return compatible, compatErr
		}
		protected := noCompatibility(compatible, compatErr)
		if protected {
			compatible.RegionSuspected = false
		}
		if directCompatible || protected {
			authoritative, authoritativeErr = compatible, compatErr
		}
		if !protected && compatErr == nil && compatible.URL != "" {
			if !compatible.CacheHit {
				r.mu.Lock()
				if r.invalidated == invalid && r.cooldownNetwork == network && activeErr() == nil {
					r.failures = 0
					r.coolUntil = time.Time{}
				}
				r.mu.Unlock()
			}
			return compatible, nil
		}
		if !protected && !cool {
			r.mu.Lock()
			if r.invalidated == invalid && r.cooldownNetwork == network && activeErr() == nil {
				r.failures++
				if r.failures >= 3 {
					r.coolUntil = time.Now().Add(time.Minute)
				}
			}
			r.mu.Unlock()
		}
	}
	if settings.GDEnabled && fallback != nil {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-fallbackReady:
		}
		if e := activeErr(); e != nil {
			return out, e
		}
		gdBudget := 17 * time.Second
		if policy.PremiumAllowed {
			gdBudget = 31 * time.Second
		}
		alternate, alternateErr := qualityStage(ctx, gdBudget, qualities, policy, false, true, id, fallback.Resolve, lookup)
		if alternateErr == nil && alternate.SongID == id && (alternate.Provider == "gd-netease" || alternate.CacheHit) && alternate.Status == "full" && alternate.URL != "" {
			return alternate, nil
		}
		// A secondary timeout must not erase a concrete Netease conclusion.
		// The fallback attempt still records its own bounded diagnostic stage.
		if alternateErr != nil && alternateErr.Error() == "GD_BUDGET_EXHAUSTED" && authoritative.ErrorCode == "" && authoritativeErr == nil && !noCompatibility(authoritative, authoritativeErr) {
			return domain.ResolvedStream{SongID: id, Status: "unavailable", ErrorCode: "GD_BUDGET_EXHAUSTED"}, alternateErr
		}
	}
	// Preserve a concrete catalog entitlement result when GD fails. Generic regional
	// experiment failure is not proof of a regional restriction.
	normal, normalErr = authoritative, authoritativeErr
	if !noCompatibility(normal, normalErr) && normal.ErrorCode == "" && normalErr != nil {
		if errors.Is(normalErr, context.DeadlineExceeded) {
			normal.ErrorCode = "RESOLVE_TIMEOUT"
		} else {
			normal.ErrorCode = normalErr.Error()
		}
	}
	return normal, normalErr
}
