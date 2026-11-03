// Package mediacache owns the app's media-only loopback channel and disk cache.
package mediacache

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/voici5986/otoneigo/internal/audioinfo"
	"github.com/voici5986/otoneigo/internal/crashreport"
	"github.com/voici5986/otoneigo/internal/domain"
)

const maxAudio = int64(512 * 1024 * 1024)

var errCache = errors.New("AUDIO_CACHE_FAILED")
var ErrInUse = errors.New("AUDIO_CACHE_IN_USE")

// IsCacheFailure identifies a cache-specific failure without classifying caller
// cancellation or unrelated transport errors as cache failures.
func IsCacheFailure(err error) bool { return errors.Is(err, errCache) }

type Options struct {
	Root        string
	Context     context.Context
	Settings    func() domain.AudioCacheSettings
	Entitlement func() domain.PlaybackEntitlement
	HTTPClient  *http.Client
	Now         func() time.Time
}

// entry is deliberately a dedicated persistence record: no URL or capability.
type entry struct {
	Key           string
	SongID        string
	Provider      string
	Quality       string
	Format        string
	Bitrate       int64
	Duration      int64
	Bytes         int64
	SHA256        string
	Chunks        []string `json:",omitempty"`
	LastAccess    int64
	Generation    string `json:",omitempty"`
	Owner         string
	durableAccess int64
	verified      time.Time
	verifying     chan struct{}
	corrupt       bool
}
type job struct {
	reserved      int64
	transient     bool
	canceled      bool
	owner         string
	stream        domain.ResolvedStream
	path          string
	size, written int64
	started, done bool
	err           error
	changed       chan struct{}
	cancel        context.CancelFunc
}
type lease struct {
	key     string
	created time.Time
	used    bool
}
type Manager struct {
	mu              sync.Mutex
	opt             Options
	ctx             context.Context
	cancel          context.CancelFunc
	client          *http.Client
	server          *http.Server
	host            string
	entries         map[string]*entry
	jobs            map[string]*job
	leases          map[string]*lease
	pins            map[string]int
	pending         map[string]bool
	invalid         map[string]bool
	available       bool
	closed          bool
	clearEpoch      uint64
	wg              sync.WaitGroup
	unlock          func() error
	recencyDirty    bool
	structuralDirty bool
	lastSaveAttempt time.Time
}

func New(o Options) (*Manager, error) {
	if o.Context == nil {
		o.Context = context.Background()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Settings == nil {
		o.Settings = domain.DefaultAudioCacheSettings
	}
	if o.Entitlement == nil {
		o.Entitlement = func() domain.PlaybackEntitlement { return domain.PlaybackEntitlement{} }
	}
	ctx, cancel := context.WithCancel(o.Context)
	m := &Manager{opt: o, ctx: ctx, cancel: cancel, entries: map[string]*entry{}, jobs: map[string]*job{}, leases: map[string]*lease{}, pending: map[string]bool{}, invalid: map[string]bool{}, pins: map[string]int{}}
	m.available = os.MkdirAll(o.Root, 0700) == nil
	if m.available {
		unlock, err := lockRoot(o.Root)
		if err != nil {
			cancel()
			return nil, err
		}
		m.unlock = unlock
		m.load()
	}
	client := http.Client{Timeout: 5 * time.Minute}
	if o.HTTPClient != nil {
		client = *o.HTTPClient
	} else {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 20 * time.Second
		tr.MaxResponseHeaderBytes = 64 * 1024
		client.Transport = tr
	}
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || !trusted(r.URL.String()) || r.URL.Host != via[0].URL.Host {
			return errCache
		}
		return nil
	}
	m.client = &client
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		cancel()
		if m.unlock != nil {
			_ = m.unlock()
		}
		return nil, errCache
	}
	m.host = listener.Addr().String()
	m.server = &http.Server{Handler: http.HandlerFunc(m.serve), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, WriteTimeout: 6 * time.Minute, MaxHeaderBytes: 16 * 1024}
	m.wg.Add(2)
	go func() {
		defer crashreport.Fatal("audio-server")
		defer m.wg.Done()
		_ = m.server.Serve(listener)
	}()
	go m.reap()
	return m, nil
}
func trusted(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && len(raw) <= 16384 && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".music.126.net") && u.Path != ""
}
func validStream(s domain.ResolvedStream) bool {
	switch s.ActualQuality {
	case "standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster":
	default:
		return false
	}
	return domain.ValidID(s.SongID) && (s.Provider == "netease-direct" || s.Provider == "gd-netease") && s.ActualBitrate > 0 && s.ActualFormat != "" && s.DurationMs > 0
}
func public(s domain.ResolvedStream) bool {
	return validStream(s) && s.ActualFormat == "mp3" && s.ActualBitrate <= 320000 && (s.ActualQuality == "standard" || s.ActualQuality == "higher" || s.ActualQuality == "exhigh")
}
func scope(e domain.PlaybackEntitlement) string {
	if !e.LoggedIn {
		return ""
	}
	if e.AccountID == "" {
		return "unidentified"
	}
	h := sha256.Sum256([]byte(e.AccountID))
	return hex.EncodeToString(h[:])
}
func allowed(s domain.ResolvedStream, owner string, e domain.PlaybackEntitlement) bool {
	if !validStream(s) || owner == "unidentified" || owner != "" && owner != scope(e) {
		return false
	}
	return public(s) || owner != "" && e.LoggedIn && e.Member && e.AccountID != ""
}
func keyFor(s domain.ResolvedStream, owner string) string {
	v := sha256.Sum256([]byte(s.SongID + "\x00" + s.Provider + "\x00" + s.ActualQuality + "\x00" + s.ActualFormat + "\x00" + strconv.FormatInt(s.ActualBitrate, 10) + "\x00" + owner))
	return hex.EncodeToString(v[:])
}
func (m *Manager) blob(key string) string { return filepath.Join(m.opt.Root, key+".blob") }
func streamFor(e *entry) domain.ResolvedStream {
	return domain.ResolvedStream{SongID: e.SongID, Provider: e.Provider, RequestedQuality: e.Quality, ActualQuality: e.Quality, ActualFormat: e.Format, ActualBitrate: e.Bitrate, DurationMs: e.Duration, ContentLengthBytes: e.Bytes, Status: "full", AccessStrategy: "cache", CacheHit: true, CacheEntryID: e.Key, URL: "cache:" + e.Key}
}

// Quality is the validated provider grade. VBR average bitrates need not
// equal the nominal 128/192/320 kbps grade exactly.
func qualityMatch(e *entry, q string) bool { return e.Quality == q }
func (m *Manager) Lookup(ctx context.Context, id, q string, ent domain.PlaybackEntitlement) (domain.ResolvedStream, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil || m.closed || !m.available || !m.opt.Settings().Enabled {
		return domain.ResolvedStream{}, false
	}
	// Verification releases mu. Snapshot eligible identities before starting it,
	// so an active map iterator never spans an unlock and newly published entries
	// belong to the next acquisition.
	var candidates []*entry
	for key, e := range m.entries {
		if !m.invalid[key] && !m.pending[key] && e.SongID == id && qualityMatch(e, q) && allowed(streamFor(e), e.Owner, ent) {
			candidates = append(candidates, e)
		}
	}
	for _, e := range candidates {
		key := e.Key
		if m.entries[key] != e || m.invalid[key] || m.pending[key] {
			continue
		}
		if !m.verifyLocked(ctx, e, false) || m.entries[key] != e || m.pending[key] || ctx.Err() != nil || !m.opt.Settings().Enabled {
			if m.entries[key] == e && (m.invalid[key] || m.pending[key]) {
				m.remove(key)
			}
			continue
		}
		m.touch(e)
		s := streamFor(e)
		s.RequestedQuality = q
		return s, true
	}
	return domain.ResolvedStream{}, false
}
func (m *Manager) Prepare(ctx context.Context, s domain.ResolvedStream) (domain.ResolvedStream, error) {
	if ctx.Err() != nil {
		return domain.ResolvedStream{}, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return domain.ResolvedStream{}, errCache
	}
	if !s.CacheHit && !trusted(s.URL) {
		return domain.ResolvedStream{}, errCache
	}
	if !m.available || !m.opt.Settings().Enabled {
		if s.CacheHit {
			return domain.ResolvedStream{}, errCache
		}
		return s, nil
	}
	// An acquisition that started before Clear must not create replacement
	// work when its unlocked integrity check later reports corruption.
	acquisitionEpoch := m.clearEpoch
	key := s.CacheEntryID
	if s.CacheHit {
		e := m.entries[key]
		if m.invalid[key] || m.pending[key] || e == nil || !allowed(streamFor(e), e.Owner, m.opt.Entitlement()) || !m.verifyLocked(ctx, e, false) || m.pending[key] || ctx.Err() != nil || !m.opt.Settings().Enabled || !allowed(streamFor(e), e.Owner, m.opt.Entitlement()) {
			return domain.ResolvedStream{}, errCache
		}
	} else {
		if !trusted(s.URL) {
			return domain.ResolvedStream{}, errCache
		}
		if s.Status != "full" || !allowed(s, scope(m.opt.Entitlement()), m.opt.Entitlement()) {
			return s, nil
		}
		switch s.ActualFormat {
		case "mp3", "flac", "wav":
		default:
			return s, nil
		}
		owner := scope(m.opt.Entitlement())
		key = keyFor(s, owner)
		if j := m.jobs[key]; j != nil && j.canceled && !j.done {
			return s, nil
		}
		// A failed blob can remain physically present for other current leases,
		// but a newly selected stream must not reuse that blob or its shared job.
		if m.invalid[key] || m.pending[key] {
			if m.active(key) {
				return s, nil
			}
			if !m.remove(key) {
				return s, nil
			}
		}
		if e := m.entries[key]; e != nil {
			ok := allowed(streamFor(e), e.Owner, m.opt.Entitlement()) && m.verifyLocked(ctx, e, false)
			if m.closed {
				return domain.ResolvedStream{}, errCache
			}
			if ctx.Err() != nil {
				return domain.ResolvedStream{}, ctx.Err()
			}
			if acquisitionEpoch != m.clearEpoch {
				return s, nil
			}
			// Clear/settings/entitlement may have changed while verification was unlocked.
			// A canceled acquisition must never recreate a cleared cache job.
			if !m.opt.Settings().Enabled || m.entries[key] != e && !m.invalid[key] || m.pending[key] && !m.invalid[key] {
				return s, nil
			}
			if !ok || !allowed(streamFor(e), e.Owner, m.opt.Entitlement()) {
				e.corrupt = true
				m.invalid[key] = true
				m.pending[key] = true
				_ = m.save()
				if m.active(key) || !m.remove(key) {
					return s, nil
				}
			} else {
				requested := s.RequestedQuality
				m.touch(e)
				s = streamFor(e)
				s.RequestedQuality = requested
			}
		}
		if _, ok := m.entries[key]; !ok {
			if s.ContentLengthBytes > maxAudio || s.ContentLengthBytes > m.opt.Settings().MaxBytes {
				return s, nil
			}
			if s.ContentLengthBytes > 0 {
				if free, ok := freeBytes(m.opt.Root); ok && free < uint64(s.ContentLengthBytes)+64*1024*1024 {
					return s, nil
				}
			}

			if j := m.jobs[key]; j == nil || j.done && j.err != nil {
				m.jobs[key] = &job{stream: s, owner: owner, path: filepath.Join(m.opt.Root, key+".part"), changed: make(chan struct{})}
			}
		}
	}
	var random [16]byte
	if _, e := rand.Read(random[:]); e != nil {
		return domain.ResolvedStream{}, errCache
	}
	token := hex.EncodeToString(random[:])
	m.leases[token] = &lease{key: key, created: m.opt.Now()}
	s.URL = "http://" + m.host + "/media/" + token
	s.CacheToken = token
	s.CacheEntryID = key
	s.ExpiresAt = 0
	return s, nil
}
func (m *Manager) signal(j *job) { close(j.changed); j.changed = make(chan struct{}) }
func (m *Manager) leased(key string) bool {
	for _, l := range m.leases {
		if l.key == key {
			return true
		}
	}
	return false
}
func (m *Manager) active(key string) bool { return m.leased(key) || m.pins[key] > 0 }
func (m *Manager) Release(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseLocked(token)
}
func (m *Manager) releaseLocked(token string) {
	l := m.leases[token]
	delete(m.leases, token)
	if l != nil {
		m.releaseKeyLocked(l.key)
	}
}
func (m *Manager) releaseKeyLocked(key string) {
	if m.leased(key) {
		return
	}
	if j := m.jobs[key]; j != nil && !j.done {
		if !j.started {
			m.remove(key)
		} else {
			j.canceled = true
			if j.cancel != nil {
				j.cancel()
			}
		}
	}
	if m.pending[key] && !m.active(key) {
		m.remove(key)
	}
	m.prune()
}
func (m *Manager) unpin(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pins[key]--
	if m.pins[key] == 0 {
		delete(m.pins, key)
	}
	m.releaseKeyLocked(key)
}
func (m *Manager) ReportFailure(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.leases[token]; l != nil {
		m.invalid[l.key] = true
		m.pending[l.key] = true
	}
	m.releaseLocked(token)
	// Exclude invalid records from persistence even while another lease keeps
	// the physical file alive. A restart must never resurrect a failed entry.
	_ = m.save()
}
func (m *Manager) Stats() domain.AudioCacheStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.opt.Settings()
	r := domain.AudioCacheStats{Available: m.available, Enabled: s.Enabled, MaxBytes: s.MaxBytes, CachedTracks: 0}
	if !m.available {
		r.ErrorCode = "AUDIO_CACHE_UNAVAILABLE"
	}
	for key, e := range m.entries {
		if !m.invalid[key] {
			r.CachedTracks++
		}
		r.UsedBytes += e.Bytes
	}
	for key, j := range m.jobs {
		if m.entries[key] == nil {
			r.UsedBytes += j.written
		}
		if j.started && !j.done {
			r.ActiveDownloads++
		}
	}
	return r
}
func (m *Manager) Clear() (domain.AudioCacheClearResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearEpoch++
	r := domain.AudioCacheClearResult{}
	failed := false
	seen := map[string]bool{}
	for k := range m.entries {
		seen[k] = true
	}
	for k := range m.jobs {
		seen[k] = true
	}
	for k := range seen {
		if m.active(k) {
			m.pending[k] = true
			r.RetainedActiveTracks++
		} else {
			if m.remove(k) {
				r.RemovedTracks++
			} else {
				failed = true
			}
		}
	}
	if err := m.save(); err != nil {
		return r, err
	}
	if failed {
		return r, errCache
	}
	return r, nil
}
func (m *Manager) ApplySettings() { m.mu.Lock(); defer m.mu.Unlock(); m.prune() }
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	_ = m.server.Close()
	m.wg.Wait()
	m.mu.Lock()
	var flushErr error
	if m.structuralDirty {
		flushErr = m.save()
	} else if m.recencyDirty {
		flushErr = m.saveRecency()
	}
	m.mu.Unlock()
	if m.unlock != nil {
		if err := m.unlock(); err != nil {
			return err
		}
	}
	return flushErr
}
func (m *Manager) reap() {
	defer crashreport.Fatal("audio-reaper")
	defer m.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			var expired []string
			m.mu.Lock()
			for token, l := range m.leases {
				if !l.used && m.opt.Now().Sub(l.created) > 30*time.Second {
					expired = append(expired, token)
				}
			}
			changed := false
			for key := range m.pending {
				if !m.active(key) && m.remove(key) {
					changed = true
				}
			}
			if changed || m.structuralDirty && m.opt.Now().Sub(m.lastSaveAttempt) >= 5*time.Second {
				_ = m.save()
			} else if !m.structuralDirty && m.recencyDirty && m.opt.Now().Sub(m.lastSaveAttempt) >= 5*time.Second {
				_ = m.saveRecency()
			}
			m.mu.Unlock()
			for _, token := range expired {
				m.Release(token)
			}
		}
	}
}

// verifyLocked enters and returns with mu held. A pin protects the file from
// eviction while hashing outside mu; entry identity prevents stale verification
// from blessing a replacement. Concurrent callers share a single verification.
func (m *Manager) verifyLocked(ctx context.Context, e *entry, existingLease bool) bool {
	for {
		if ctx.Err() != nil || m.closed || m.entries[e.Key] != e || e.corrupt || m.invalid[e.Key] && !existingLease {
			return false
		}
		if ch := e.verifying; ch != nil {
			m.mu.Unlock()
			select {
			case <-ch:
			case <-ctx.Done():
			case <-m.ctx.Done():
			}
			m.mu.Lock()
			continue
		}
		break
	}
	ch := make(chan struct{})
	e.verifying = ch
	snapshot := *e
	m.pins[e.Key]++
	m.wg.Add(1)
	m.mu.Unlock()
	stamp, chunks, ok := m.verifyFile(ctx, &snapshot)
	m.mu.Lock()
	e.verifying = nil
	close(ch)
	m.pins[e.Key]--
	if m.pins[e.Key] == 0 {
		delete(m.pins, e.Key)
	}
	m.wg.Done()
	current := m.entries[e.Key] == e
	if current && ok {
		e.verified = stamp
		if len(e.Chunks) == 0 && len(chunks) > 0 && !m.pending[e.Key] {
			e.Chunks = chunks
			_ = m.save()
		}
	}
	if current && !ok && ctx.Err() == nil && !m.closed {
		e.corrupt = true
		m.invalid[e.Key] = true
		m.pending[e.Key] = true
		_ = m.save()
	}
	return ok && current && !m.closed && (!m.invalid[e.Key] || existingLease) && ctx.Err() == nil && m.entries[e.Key] == e
}

func (m *Manager) verifyFile(ctx context.Context, e *entry) (time.Time, []string, bool) {
	f, err := os.Open(m.blob(e.Key))
	if err != nil {
		return time.Time{}, nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() != e.Bytes {
		return time.Time{}, nil, false
	}
	if len(e.Chunks) > 0 {
		if !validChunks(e) {
			return time.Time{}, nil, false
		}
		_, err = audioinfo.Inspect(newChunkReader(ctx, f, e, nil), e.Bytes)
		return st.ModTime(), nil, err == nil && ctx.Err() == nil
	}
	if !e.verified.IsZero() && e.verified.Equal(st.ModTime()) {
		return st.ModTime(), nil, true
	}
	h := sha256.New()
	var chunks chunkDigests
	buf := make([]byte, 128*1024)
	for {
		if ctx.Err() != nil || m.ctx.Err() != nil {
			return time.Time{}, nil, false
		}
		n, err := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
			chunks.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return time.Time{}, nil, false
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
		return time.Time{}, nil, false
	}
	if _, err = audioinfo.Inspect(f, e.Bytes); err != nil {
		return time.Time{}, nil, false
	}
	after, err := f.Stat()
	if err != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		return time.Time{}, nil, false
	}
	return st.ModTime(), chunks.finish(), true
}
func (m *Manager) touch(e *entry) {
	if now := m.opt.Now().UnixMilli(); now > e.LastAccess {
		e.LastAccess = now
	}
	m.recencyDirty = true
}
func (m *Manager) save() error {
	// Keep structural mutations retryable if any stage of persistence fails.
	m.structuralDirty = true
	// Bound background retries even if the filesystem rejects the write.
	m.lastSaveAttempt = m.opt.Now()
	records := make(map[string]*entry, len(m.entries))
	for key, e := range m.entries {
		if !m.invalid[key] && !m.pending[key] {
			if !validGeneration(e.Generation) {
				var generation [16]byte
				if _, err := rand.Read(generation[:]); err != nil {
					return errCache
				}
				e.Generation = hex.EncodeToString(generation[:])
			}
			records[key] = e
		}
	}
	b, e := json.Marshal(records)
	if e != nil || len(b) > maxManifest {
		return errCache
	}
	if err := m.writeSnapshot("manifest", b); err != nil {
		return err
	}
	for _, e := range records {
		e.durableAccess = e.LastAccess
	}
	m.recencyDirty = false
	m.structuralDirty = false
	// A crash before removal is harmless: overlays also bind the structural
	// access baseline, and never create entries absent from the manifest.
	_ = os.Remove(filepath.Join(m.opt.Root, "recency.json"))
	return nil
}
func validKey(k string) bool {
	b, e := hex.DecodeString(k)
	return e == nil && len(b) == 32 && strings.ToLower(k) == k
}
func (m *Manager) load() {
	f, e := os.Open(filepath.Join(m.opt.Root, "manifest.json"))
	var b []byte
	if e == nil {
		b, e = io.ReadAll(io.LimitReader(f, maxManifest+1))
		_ = f.Close()
	}
	if e == nil && len(b) <= maxManifest {
		var es map[string]*entry
		if json.Unmarshal(b, &es) == nil {
			for k, v := range es {
				if v != nil && validKey(k) && v.Key == k && v.Bytes > 0 && v.Bytes <= maxAudio && validStream(streamFor(v)) && keyFor(streamFor(v), v.Owner) == k && validKey(v.SHA256) && (len(v.Chunks) == 0 || validChunks(v)) {
					// Optional recency identity corruption must not discard audio.
					if !validGeneration(v.Generation) {
						v.Generation = ""
					}
					v.durableAccess = v.LastAccess
					m.entries[k] = v
				}
			}
		}
	}
	m.loadRecency()
	files, _ := os.ReadDir(m.opt.Root)
	for _, f := range files {
		ext := filepath.Ext(f.Name())
		k := strings.TrimSuffix(f.Name(), ext)
		if !f.IsDir() && validKey(k) && (ext == ".part" || ext == ".blob" && m.entries[k] == nil) {
			_ = os.Remove(filepath.Join(m.opt.Root, f.Name()))
		}
	}
	m.prune()
	_ = m.save()
}

// Keep the record and physical-byte accounting until deletion succeeds.
// Windows can refuse unlink while a native reader still holds the file.
func (m *Manager) remove(k string) bool {
	if m.active(k) {
		return false
	}
	if j := m.jobs[k]; j != nil && j.started && !j.done {
		j.canceled = true
		if j.cancel != nil {
			j.cancel()
		}
		return false
	}
	for _, p := range []string{m.blob(k), filepath.Join(m.opt.Root, k+".part")} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			m.pending[k] = true
			m.structuralDirty = true
			return false
		}
	}
	if m.entries[k] != nil {
		m.structuralDirty = true
	}
	delete(m.entries, k)
	delete(m.jobs, k)
	delete(m.pending, k)
	delete(m.invalid, k)
	return true
}
func (m *Manager) prune() {
	changed := false
	var used int64
	for _, j := range m.jobs {
		if j.started && !j.done {
			used += j.reserved
		}
	}
	var es []*entry
	for _, e := range m.entries {
		used += e.Bytes
		es = append(es, e)
	}
	sort.Slice(es, func(i, j int) bool { return es[i].LastAccess < es[j].LastAccess })
	limit := m.opt.Settings().MaxBytes
	for _, e := range es {
		if used <= limit {
			break
		}
		if !m.active(e.Key) {
			// Failed Windows unlink still marks the record pending and must
			// durably exclude it from restart eligibility.
			changed = true
			if m.remove(e.Key) {
				used -= e.Bytes
			}
		}
	}
	if changed {
		_ = m.save()
	}
}

func (m *Manager) download(key string, j *job) {
	defer crashreport.Fatal("audio-download")
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	m.mu.Lock()
	j.cancel = cancel
	if j.canceled {
		cancel()
	}
	m.mu.Unlock()
	defer cancel()
	var failure error = errCache
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if failure != nil {
			j.err = errCache
			j.stream.URL = ""
			j.done = true
			m.pending[key] = true
			_ = os.Remove(j.path)
			m.signal(j)
			if !m.active(key) {
				m.remove(key)
			}
		}
	}()
	// This guard runs before failure cleanup takes mu. An invariant panic may
	// have left mu held; trying to clean it up first would hide the crash forever.
	defer crashreport.Fatal("audio-download")
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, j.stream.URL, nil)
	if e != nil {
		return
	}
	req.Header.Set("Accept-Encoding", "identity")
	var resp *http.Response
	if crashreport.Contain("audio-fetch", func() { resp, e = m.client.Do(req) }) {
		return
	}
	if e != nil {
		return
	}
	defer func() {
		crashreport.Contain("audio-fetch", func() { _ = resp.Body.Close() })
	}()
	size := resp.ContentLength
	if resp.StatusCode != 200 || size <= 0 || size > maxAudio || j.stream.ContentLengthBytes > 0 && j.stream.ContentLengthBytes != size {
		return
	}
	m.mu.Lock()
	j.reserved = size
	j.transient = size > m.opt.Settings().MaxBytes
	m.prune()
	m.mu.Unlock()
	if free, ok := freeBytes(m.opt.Root); ok && free < uint64(size)+64*1024*1024 {
		return
	}
	f, e := os.OpenFile(j.path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	h := sha256.New()
	var chunks chunkDigests
	buf := make([]byte, 64*1024)
	var written int64
	prefix := make([]byte, 0, audioinfo.PrefixLimit)
	prefixOK := false
	for {
		var n int
		var re error
		if crashreport.Contain("audio-fetch", func() { n, re = resp.Body.Read(buf) }) {
			return
		}
		if n > 0 {
			if written+int64(n) > size {
				return
			}
			if _, e = f.Write(buf[:n]); e != nil {
				return
			}
			_, _ = h.Write(buf[:n])
			chunks.Write(buf[:n])
			written += int64(n)
			if !prefixOK {
				remaining := audioinfo.PrefixLimit - len(prefix)
				take := n
				if take > remaining {
					take = remaining
				}
				prefix = append(prefix, buf[:take]...)
				if info, err := audioinfo.InspectPrefix(prefix, size); err == nil {
					if info.Format != j.stream.ActualFormat {
						return
					}
					prefixOK = true
					prefix = nil
				}
				if !prefixOK && (len(prefix) == audioinfo.PrefixLimit || written == size) {
					return
				}
			}
			m.mu.Lock()
			if prefixOK {
				j.size = size
			}
			j.written = written
			m.signal(j)
			m.mu.Unlock()
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
	if written != size {
		return
	}
	info, e := audioinfo.Inspect(f, size)
	if e != nil {
		return
	}
	if info.Format != j.stream.ActualFormat {
		return
	}
	if public(j.stream) && (info.Lossless || info.Bitrate > 320000 || math.Abs(float64(info.Bitrate-j.stream.ActualBitrate)) > math.Max(1000, float64(j.stream.ActualBitrate)*.02)) {
		return
	}
	tol := math.Max(2000, float64(j.stream.DurationMs)*.02)
	if j.stream.DurationMs <= 0 || math.Abs(float64(info.DurationMs-j.stream.DurationMs)) > tol {
		return
	}
	if f.Sync() != nil || f.Close() != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if j.canceled || ctx.Err() != nil {
		return
	}
	if j.transient {
		j.done = true
		j.stream.URL = ""
		failure = nil
		m.signal(j)
		if !m.active(key) {
			m.remove(key)
		}
		return
	}
	if os.Rename(j.path, m.blob(key)) != nil {
		return
	}
	j.path = m.blob(key)
	j.done = true
	j.stream.URL = ""
	en := &entry{Key: key, Owner: j.owner, SongID: j.stream.SongID, Provider: j.stream.Provider, Quality: j.stream.ActualQuality, Format: info.Format, Bitrate: j.stream.ActualBitrate, Duration: info.DurationMs, Bytes: size, SHA256: hex.EncodeToString(h.Sum(nil)), Chunks: chunks.finish(), LastAccess: m.opt.Now().UnixMilli()}
	m.entries[key] = en
	failure = nil
	m.signal(j)
	if m.pending[key] && !m.active(key) {
		m.remove(key)
	}
	m.prune()
	_ = m.save()
}
func parseRange(s string, size int64) (int64, int64, error) {
	if s == "" {
		return 0, size - 1, nil
	}
	if !strings.HasPrefix(s, "bytes=") || strings.Contains(s, ",") {
		return 0, 0, errCache
	}
	a, b, ok := strings.Cut(strings.TrimPrefix(s, "bytes="), "-")
	if !ok {
		return 0, 0, errCache
	}
	if a == "" {
		n, e := strconv.ParseInt(b, 10, 64)
		if e != nil || n <= 0 {
			return 0, 0, errCache
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, nil
	}
	start, e := strconv.ParseInt(a, 10, 64)
	if e != nil || start < 0 || start >= size {
		return 0, 0, errCache
	}
	end := size - 1
	if b != "" {
		end, e = strconv.ParseInt(b, 10, 64)
		if e != nil || end < start {
			return 0, 0, errCache
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}
func (m *Manager) serve(w http.ResponseWriter, r *http.Request) {
	defer crashreport.FatalHTTP("audio-handler")
	if r.Host != m.host || r.URL.RawQuery != "" || r.URL.RawPath != "" || !strings.HasPrefix(r.URL.Path, "/media/") {
		http.NotFound(w, r)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/media/")
	if len(token) != 32 {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	m.mu.Lock()
	l := m.leases[token]
	if l == nil || m.closed {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	key := l.key
	m.pins[key]++
	m.wg.Add(1)
	defer func() { m.unpin(key); m.wg.Done() }()
	// A panic while mu is held must be recorded before unpin tries to lock it.
	defer crashreport.FatalHTTP("audio-handler")
	l.used = true
	en := m.entries[key]
	j := m.jobs[key]
	if en != nil && !allowed(streamFor(en), en.Owner, m.opt.Entitlement()) || en == nil && (j == nil || !allowed(j.stream, j.owner, m.opt.Entitlement())) {
		m.mu.Unlock()
		w.WriteHeader(403)
		return
	}
	if en != nil {
		if !m.verifyLocked(r.Context(), en, true) || !allowed(streamFor(en), en.Owner, m.opt.Entitlement()) {
			m.mu.Unlock()
			w.WriteHeader(410)
			return
		}
		start, _, rangeErr := parseRange(r.Header.Get("Range"), en.Bytes)
		if rangeErr != nil {
			size := en.Bytes
			m.mu.Unlock()
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.WriteHeader(416)
			return
		}
		m.touch(en)
		p := m.blob(key)
		m.mu.Unlock()
		f, e := os.Open(p)
		if e != nil {
			w.WriteHeader(410)
			return
		}
		defer f.Close()
		w.Header().Set("Cache-Control", "no-store")
		reader := newChunkReader(r.Context(), f, en, func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.entries[key] == en && !m.closed {
				en.corrupt = true
				m.invalid[key] = true
				m.pending[key] = true
				_ = m.save()
			}
		})
		if !validChunks(en) || reader.load(start/integrityChunk) != nil {
			w.WriteHeader(410)
			return
		}
		reader.abort = true
		w.Header().Set("Content-Type", mimeFor(en.Format))
		http.ServeContent(w, r, "audio", time.Time{}, reader)
		return
	}
	if !j.started {
		j.started = true
		m.wg.Add(1)
		go m.download(key, j)
	}
	for j.size == 0 && !j.done {
		ch := j.changed
		m.mu.Unlock()
		select {
		case <-ch:
		case <-r.Context().Done():
			return
		}
		m.mu.Lock()
	}
	if j.size == 0 || j.err != nil {
		m.mu.Unlock()
		w.WriteHeader(502)
		return
	}
	size := j.size
	m.mu.Unlock()
	start, end, e := parseRange(r.Header.Get("Range"), size)
	if e != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(416)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", mimeFor(j.stream.ActualFormat))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	status := 200
	if r.Header.Get("Range") != "" {
		status = 206
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}
	w.WriteHeader(status)
	if r.Method == "HEAD" {
		return
	}
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	var reader *os.File
	defer func() {
		if reader != nil {
			_ = reader.Close()
		}
	}()
	buf := make([]byte, 64*1024)
	for pos := start; pos <= end; {
		m.mu.Lock()
		if j.err != nil {
			m.mu.Unlock()
			panic(http.ErrAbortHandler)
		}
		available := j.written - pos
		if available <= 0 {
			if j.done {
				m.mu.Unlock()
				return
			}
			ch := j.changed
			m.mu.Unlock()
			select {
			case <-ch:
			case <-r.Context().Done():
				return
			}
			continue
		}
		n := int64(len(buf))
		if n > available {
			n = available
		}
		if n > end-pos+1 {
			n = end - pos + 1
		}
		if reader == nil {
			reader, e = openStreamReader(j.path)
		}
		m.mu.Unlock()
		var got int
		if e == nil {
			got, e = reader.ReadAt(buf[:n], pos)
		}
		if e != nil && e != io.EOF || got == 0 {
			return
		}
		if _, e = w.Write(buf[:got]); e != nil {
			return
		}
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		pos += int64(got)
	}
}
func mimeFor(f string) string {
	switch f {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "wav":
		return "audio/wav"
	case "m4a":
		return "audio/mp4"
	case "ogg":
		return "audio/ogg"
	}
	return "application/octet-stream"
}
