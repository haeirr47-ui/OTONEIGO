// Package gd implements the optional, anonymous GD Netease-only fallback.
// Returned addresses and all caches are memory-only. Same ID and duration do
// not establish recording-version identity; native playback remains a gate.
package gd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/voici5986/otoneigo/internal/audioinfo"
	"github.com/voici5986/otoneigo/internal/domain"
)

const endpoint = "https://music-api.gdstudio.xyz/api.php"
const probeLimit = 64 * 1024
const maxMediaSize = 512 * 1024 * 1024
const requestTimeout = 3 * time.Second
const minAttemptBudget = 4 * time.Second

var errBudgetExhausted = errors.New("GD_BUDGET_EXHAUSTED")

type Metadata interface {
	GetSong(context.Context, string) (domain.TrackSnapshot, error)
}
type Options struct {
	// Transport supports local fixtures without changing logical, validated URLs.
	Transport http.RoundTripper
	Now       func() time.Time
	// Values below the public-service minimum are clamped to six seconds.
	RateInterval time.Duration
	CacheTTL     time.Duration
	Cooldown     time.Duration
}
type cached struct {
	value domain.ResolvedStream
	until time.Time
}
type flight struct {
	done       chan struct{}
	cancel     context.CancelFunc
	waiters    int
	callers    map[context.Context]int
	generation uint64
	value      domain.ResolvedStream
	err        error
}
type verifiedMetadata struct {
	duration int64
	until    time.Time
}
type Client struct {
	metadata                Metadata
	api, media              *http.Client
	now                     func() time.Time
	interval, ttl, cooldown time.Duration
	mu                      sync.Mutex
	generation              uint64
	cache                   map[string]cached
	metadataCache           map[string]verifiedMetadata
	flights                 map[string]*flight
	next                    time.Time
	failures                int
	coolUntil               time.Time
	closed                  bool
}

func NewClient(m Metadata) *Client { return NewClientWithOptions(m, Options{}) }
func NewClientWithOptions(m Metadata, o Options) *Client {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RateInterval < 6*time.Second {
		o.RateInterval = 6 * time.Second
	}
	if o.CacheTTL <= 0 || o.CacheTTL > 30*time.Second {
		o.CacheTTL = 30 * time.Second
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 30 * time.Second
	}
	transport := o.Transport
	if transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.MaxResponseHeaderBytes = 32 * 1024
		transport = t
	}
	c := &Client{metadata: m, now: o.Now, interval: o.RateInterval, ttl: o.CacheTTL, cooldown: o.Cooldown, cache: map[string]cached{}, metadataCache: map[string]verifiedMetadata{}, flights: map[string]*flight{}}
	c.api = &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("GD_API_REDIRECT") }}
	c.media = &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !trustedMedia(r.URL.String()) {
			return errors.New("UNTRUSTED_MEDIA_URL")
		}
		return nil
	}}
	return c
}
func bitrate(q string) string {
	switch q {
	case "standard":
		return "128"
	case "higher":
		return "192"
	case "exhigh":
		return "320"
	case "lossless":
		return "740"
	case "hires", "jyeffect", "sky", "jymaster":
		return "999"
	}
	return ""
}
func (c *Client) Resolve(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
	if !domain.ValidID(id) {
		return domain.ResolvedStream{}, errors.New("INVALID_SONG_ID")
	}
	if bitrate(q) == "" {
		return domain.ResolvedStream{}, errors.New("INVALID_QUALITY")
	}
	if err := ctx.Err(); err != nil {
		return domain.ResolvedStream{}, err
	}
	key := id + ":" + q
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return domain.ResolvedStream{}, context.Canceled
	}
	now := c.now()
	for k, v := range c.cache {
		if !now.Before(v.until) {
			delete(c.cache, k)
		}
	}
	if v, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return v.value, nil
	}
	f := c.flights[key]
	if f == nil {
		if now.Before(c.coolUntil) || len(c.flights) >= 16 {
			c.mu.Unlock()
			return domain.ResolvedStream{}, errors.New("GD_RATE_LIMITED")
		}
		// Rate waiting belongs to the resolver stage, not the three-second HTTP cap.
		shared, cancel := context.WithTimeout(context.Background(), 31*time.Second)
		f = &flight{done: make(chan struct{}), cancel: cancel, generation: c.generation, callers: map[context.Context]int{}}
		c.flights[key] = f
		go c.run(shared, key, id, q, f)
	}
	f.waiters++
	f.callers[ctx]++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		f.waiters--
		f.callers[ctx]--
		if f.callers[ctx] == 0 {
			delete(f.callers, ctx)
		}
		if f.waiters == 0 {
			f.cancel()
			if c.flights[key] == f {
				delete(c.flights, key)
			}
		}
	}()
	select {
	case <-ctx.Done():
		return domain.ResolvedStream{}, ctx.Err()
	case <-f.done:
		c.mu.Lock()
		stale := c.closed || f.generation != c.generation
		c.mu.Unlock()
		if stale {
			return domain.ResolvedStream{}, context.Canceled
		}
		if err := ctx.Err(); err != nil {
			return domain.ResolvedStream{}, err
		}
		return f.value, f.err
	}
}
func (c *Client) run(ctx context.Context, key, id, q string, f *flight) {
	v, err := c.resolve(ctx, id, q, f)
	c.mu.Lock()
	defer c.mu.Unlock()
	defer f.cancel()
	if f.generation != c.generation || c.flights[key] != f || ctx.Err() != nil {
		v = domain.ResolvedStream{}
		if err == nil {
			err = context.Canceled
		}
	}
	f.value, f.err = v, err
	if c.flights[key] == f {
		delete(c.flights, key)
	}
	if f.generation == c.generation && !c.closed && ctx.Err() == nil {
		if err == nil {
			if len(c.cache) >= 128 {
				clear(c.cache)
			}
			c.cache[key] = cached{v, c.now().Add(c.ttl)}
			c.failures = 0
		} else if !qualityMiss(err) && !errors.Is(err, errBudgetExhausted) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			c.failures++
			if c.failures >= 3 {
				c.coolUntil = c.now().Add(c.cooldown)
			}
		}
	}
	close(f.done)
}

// Missing media at one quality says nothing about service health at lower tiers.
func qualityMiss(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "GD_QUALITY_UNAVAILABLE", "GD_UNSUPPORTED_FORMAT", "GD_DURATION_UNVERIFIED":
		return true
	}
	return false
}
func (c *Client) Invalidate() { c.mu.Lock(); defer c.mu.Unlock(); c.invalidate() }
func (c *Client) invalidate() {
	c.generation++
	clear(c.cache)
	clear(c.metadataCache)
	for _, f := range c.flights {
		f.cancel()
	}
	clear(c.flights)
	c.failures = 0
	c.coolUntil = time.Time{}
}
func (c *Client) Close() { c.mu.Lock(); defer c.mu.Unlock(); c.closed = true; c.invalidate() }

// Reserve at dispatch, not on arrival: cancelled waiters never consume future slots.
func (c *Client) rate(ctx context.Context, flights ...*flight) error {
	for {
		c.mu.Lock()
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return err
		}
		now := c.now()
		delay := c.next.Sub(now)
		if delay < 0 {
			delay = 0
		}
		// Reject a rate wait that leaves no useful execution budget. The four
		// seconds are an admission floor, not a guarantee that both three-second
		// HTTP caps fit. Shared HTTP work retains its own cap so a later caller
		// cannot inherit the first caller's shorter deadline.
		if len(flights) > 0 {
			available := time.Duration(0)
			for caller := range flights[0].callers {
				if caller.Err() != nil {
					continue
				}
				deadline, bounded := caller.Deadline()
				remaining := time.Until(deadline) - delay
				if !bounded {
					remaining = 2 * requestTimeout
				}
				if remaining > available {
					available = remaining
				}
			}
			if deadline, bounded := ctx.Deadline(); bounded {
				available = min(available, time.Until(deadline)-delay)
			}
			if available < minAttemptBudget {
				c.mu.Unlock()
				return errBudgetExhausted
			}
		}
		if delay <= 0 {
			c.next = now.Add(c.interval)
			c.mu.Unlock()
			return ctx.Err()
		}
		c.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (c *Client) resolve(ctx context.Context, id, q string, f *flight) (domain.ResolvedStream, error) {
	fail := func(code string) (domain.ResolvedStream, error) { return domain.ResolvedStream{}, errors.New(code) }
	if c.metadata == nil {
		return fail("GD_METADATA_UNVERIFIED")
	}
	c.mu.Lock()
	meta, ok := c.metadataCache[id]
	ok = ok && c.now().Before(meta.until)
	c.mu.Unlock()
	if !ok {
		metadataCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		track, err := c.metadata.GetSong(metadataCtx, id)
		metadataErr := metadataCtx.Err()
		cancel()
		if ctx.Err() != nil {
			return domain.ResolvedStream{}, ctx.Err()
		}
		if err != nil || metadataErr != nil || track.NeteaseSongID != id || track.DurationMs <= 0 || track.DurationMs > 86400000 {
			return fail("GD_METADATA_UNVERIFIED")
		}
		meta = verifiedMetadata{duration: track.DurationMs, until: c.now().Add(c.ttl)}
		c.mu.Lock()
		if f.generation == c.generation && !c.closed && ctx.Err() == nil {
			if len(c.metadataCache) >= 128 {
				clear(c.metadataCache)
			}
			c.metadataCache[id] = meta
		}
		c.mu.Unlock()
	}
	if err := c.rate(ctx, f); err != nil {
		return domain.ResolvedStream{}, err
	}
	u, _ := url.Parse(endpoint)
	params := url.Values{"types": {"url"}, "source": {"netease"}, "id": {id}, "br": {bitrate(q)}}
	u.RawQuery = params.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Accept", "application/json")
	resp, err := c.api.Do(req)
	if err != nil {
		return fail("GD_REQUEST_FAILED")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail("GD_UNAVAILABLE")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024+1))
	if err != nil || len(data) > 16*1024 {
		return fail("INVALID_PROVIDER_RESPONSE")
	}
	var payload struct {
		URL string      `json:"url"`
		BR  json.Number `json:"br"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return fail("INVALID_PROVIDER_RESPONSE")
	}
	if payload.URL == "" && (payload.BR.String() == "0" || payload.BR.String() == "128" || payload.BR.String() == "192" || payload.BR.String() == "320" || payload.BR.String() == "740" || payload.BR.String() == "999") {
		return fail("GD_QUALITY_UNAVAILABLE")
	}
	switch payload.BR.String() {
	case "128", "192", "320", "740", "999":
	default:
		return fail("INVALID_PROVIDER_RESPONSE")
	}
	if !trustedMedia(payload.URL) {
		return fail("UNTRUSTED_MEDIA_URL")
	}
	info, length, err := c.probe(ctx, payload.URL)
	if err != nil {
		return domain.ResolvedStream{}, err
	}
	duration, bits := info.DurationMs, info.Bitrate
	tolerance := meta.duration / 50
	if tolerance < 2000 {
		tolerance = 2000
	}
	delta := duration - meta.duration
	if delta < 0 {
		delta = -delta
	}
	if delta > tolerance {
		return fail("GD_DURATION_UNVERIFIED")
	}
	actual := "standard"
	if info.Format == "flac" || info.Format == "wav" {
		actual = "lossless"
		if info.BitsPerSample >= 24 || info.SampleRate > 48000 {
			actual = "hires"
		}
	} else if bits >= 320000 {
		actual = "exhigh"
	} else if bits >= 192000 {
		actual = "higher"
	}
	return domain.ResolvedStream{SongID: id, Provider: "gd-netease", AccessStrategy: "normal", URL: payload.URL, RequestedQuality: q, ActualQuality: actual, ActualFormat: info.Format, ActualBitrate: bits, Status: "full", DurationMs: duration, ContentLengthBytes: length, ExpiresAt: c.now().Add(c.ttl).UnixMilli()}, nil
}
func trustedMedia(raw string) bool {
	if len(raw) > 8192 || strings.Contains(raw, "#") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if len(host) <= len(".music.126.net") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && u.Opaque == "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".music.126.net") && u.Host == u.Hostname() && u.Path != ""
}
func (c *Client) probe(ctx context.Context, raw string) (audioinfo.Info, int64, error) {
	bad := func() (audioinfo.Info, int64, error) { return audioinfo.Info{}, 0, errors.New("GD_MEDIA_UNVERIFIED") }
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	req.Header.Set("Range", "bytes=0-65535")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.media.Do(req)
	if err != nil {
		return bad()
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 || resp.Uncompressed || resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
		return bad()
	}
	total := resp.ContentLength
	if resp.StatusCode == 206 {
		s := strings.TrimPrefix(resp.Header.Get("Content-Range"), "bytes 0-")
		parts := strings.Split(s, "/")
		if len(parts) != 2 {
			return bad()
		}
		end, e := strconv.ParseInt(parts[0], 10, 64)
		if e != nil || end < 0 || end >= probeLimit {
			return bad()
		}
		total, e = strconv.ParseInt(parts[1], 10, 64)
		if e != nil || total <= end || resp.ContentLength != end+1 {
			return bad()
		}
	}
	if total <= 0 || total > maxMediaSize {
		return bad()
	}
	sample, err := io.ReadAll(io.LimitReader(resp.Body, probeLimit+1))
	if err != nil || len(sample) > probeLimit {
		return bad()
	}
	// A server ignoring Range is allowed only when the entire response fits the bound.
	if resp.ContentLength != int64(len(sample)) {
		return bad()
	}
	info, inspectErr := audioinfo.InspectPrefix(sample, total)
	if errors.Is(inspectErr, audioinfo.ErrUnsupported) {
		return audioinfo.Info{}, 0, errors.New("GD_UNSUPPORTED_FORMAT")
	}
	if inspectErr != nil {
		return bad()
	}
	return info, total, nil
}
