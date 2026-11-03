// Package resourcecache stores only public artwork and lyrics. Its HTTP handler
// plugs into the application asset pipeline; it never opens a listener.
package resourcecache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/voici5986/otoneigo/internal/crashreport"
	"github.com/voici5986/otoneigo/internal/domain"
)

const (
	maxCover    = 4 * 1024 * 1024
	maxLyric    = 1024 * 1024
	maxEntries  = 10000
	maxManifest = 4 * 1024 * 1024
	diskMargin  = 32 * 1024 * 1024
	lyricTTL    = 7 * 24 * time.Hour
)

var (
	errCache    = errors.New("RESOURCE_CACHE_UNAVAILABLE")
	ErrInUse    = errors.New("RESOURCE_CACHE_IN_USE")
	errInvalid  = errors.New("INVALID_RESOURCE")
	hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Options struct {
	Root       string
	Context    context.Context
	Settings   func() domain.ResourceCacheSettings
	HTTPClient *http.Client
	Now        func() time.Time
}

// Entries contain opaque hashes and validation metadata, never source URLs or IDs.
type entry struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Digest  string `json:"digest"`
	Size    int64  `json:"size"`
	Created int64  `json:"created"`
	Access  int64  `json:"access"`
}

// verifiedCover binds a complete image validation to its exact bytes. It is
// memory-only and bounded by entries, rather than trusting persisted MIME data.
type verifiedCover struct {
	digest string
	mime   string
}

type job struct {
	done chan struct{}
	data []byte
	err  error
}

type validation struct {
	done       chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	generation uint64
	valid      bool // guarded by Manager.mu; false also covers invalidated work
}

type Manager struct {
	mu                sync.Mutex
	root              string
	ctx               context.Context
	cancel            context.CancelFunc
	settings          func() domain.ResourceCacheSettings
	policy            domain.ResourceCacheSettings
	client            *http.Client
	now               func() time.Time
	entries           map[string]entry
	verified          map[string]verifiedCover
	validating        map[string]*validation
	refreshAfter      map[string]time.Time
	jobs              map[string]*job
	coverWorkers      chan struct{}
	validationWorkers chan struct{}
	lyricWorkers      chan struct{}
	wg                sync.WaitGroup
	unlock            func() error
	generation        uint64
	closed            bool
	errorCode         string
}

func New(o Options) (*Manager, error) {
	if o.Root == "" {
		return nil, errCache
	}
	if o.Context == nil {
		o.Context = context.Background()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Settings == nil {
		o.Settings = domain.DefaultResourceCacheSettings
	}
	if err := os.MkdirAll(o.Root, 0700); err != nil {
		return nil, errCache
	}
	st, err := os.Lstat(o.Root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, errCache
	}
	unlock, err := lockRoot(o.Root)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(o.Context)
	client := http.Client{Timeout: 25 * time.Second}
	if o.HTTPClient != nil {
		client = *o.HTTPClient
	}
	// No credential jar and no redirect, including cross-host redirects.
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m := &Manager{root: o.Root, ctx: ctx, cancel: cancel, settings: o.Settings, client: &client, now: o.Now, entries: map[string]entry{}, verified: map[string]verifiedCover{}, refreshAfter: map[string]time.Time{}, jobs: map[string]*job{}, coverWorkers: make(chan struct{}, 4), validationWorkers: make(chan struct{}, 2), lyricWorkers: make(chan struct{}, 2), unlock: unlock}
	m.policy = m.currentSettings()
	m.validating = map[string]*validation{}
	m.load()
	m.pruneLocked(0)
	if err := m.persistLocked(); err != nil {
		m.errorCode = "RESOURCE_CACHE_UNAVAILABLE"
	}
	return m, nil
}

func (m *Manager) currentSettings() domain.ResourceCacheSettings {
	v := domain.NormalizeResourceCacheSettings(m.settings())
	if domain.ValidateResourceCacheSettings(v) != nil {
		return domain.DefaultResourceCacheSettings()
	}
	return v
}

func keyFor(kind, ref string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + ref))
	return hex.EncodeToString(h[:])
}

func (m *Manager) filename(key string) string { return filepath.Join(m.root, key+".bin") }

func readBounded(path string, limit int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errCache
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errCache
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errCache
	}
	return b, nil
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func coverMIME(b []byte) (string, error) {
	if len(b) == 0 || len(b) > maxCover {
		return "", errInvalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 32*1024*1024 {
		return "", errInvalid
	}
	// DecodeConfig accepts image headers without pixel data. Require a complete
	// decodable image after checking dimensions, before it can enter the cache.
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(b))
	if err != nil || decodedFormat != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return "", errInvalid
	}
	switch format {
	case "jpeg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	case "gif":
		return "image/gif", nil
	}
	return "", errInvalid
}

func lyricBytes(v domain.LyricData) ([]byte, error) {
	if !utf8.ValidString(v.Original) || !utf8.ValidString(v.Translated) || len(v.Original)+len(v.Translated) > maxLyric {
		return nil, errInvalid
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxLyric {
		return nil, errInvalid
	}
	return b, nil
}

func validData(kind string, b []byte) bool {
	if kind == "cover" {
		_, err := coverMIME(b)
		return err == nil
	}
	if kind != "lyric" || !utf8.Valid(b) || len(b) > maxLyric {
		return false
	}
	var v domain.LyricData
	if json.Unmarshal(b, &v) != nil {
		return false
	}
	_, err := lyricBytes(v)
	return err == nil && strings.TrimSpace(v.Original+v.Translated) != ""
}

func (m *Manager) load() {
	b, err := readBounded(filepath.Join(m.root, "manifest.json"), maxManifest)
	var rows []entry
	if err == nil {
		_ = json.Unmarshal(b, &rows)
	}
	if len(rows) > maxEntries {
		rows = nil
	}
	for _, e := range rows {
		if !hashPattern.MatchString(e.Key) || !hashPattern.MatchString(e.Digest) || (e.Kind != "cover" && e.Kind != "lyric") || e.Size <= 0 || e.Size > maxCover || (e.Kind == "lyric" && e.Size > maxLyric) || e.Created <= 0 || e.Access <= 0 {
			continue
		}
		// Startup only inspects bounded manifest metadata and file attributes.
		// Payloads remain untrusted until first use, including when disabled.
		st, err := os.Lstat(m.filename(e.Key))
		if err != nil || !st.Mode().IsRegular() || st.Size() != e.Size {
			continue
		}
		m.entries[e.Key] = e
	}
	// Cleanup only follows acquisition of the process lifetime lock.
	files, _ := os.ReadDir(m.root)
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, ".tmp-") || (strings.HasSuffix(name, ".bin") && hashPattern.MatchString(strings.TrimSuffix(name, ".bin")) && m.entries[strings.TrimSuffix(name, ".bin")].Key == "") {
			_ = os.Remove(filepath.Join(m.root, name))
		}
	}
}

func atomicWrite(root, path string, b []byte) error {
	f, err := os.CreateTemp(root, ".tmp-")
	if err != nil {
		return errCache
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil || ce != nil {
		return errCache
	}
	if os.Rename(tmp, path) != nil {
		return errCache
	}
	return nil
}

func (m *Manager) persistLocked() error {
	rows := make([]entry, 0, len(m.entries))
	for _, e := range m.entries {
		rows = append(rows, e)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	b, err := json.Marshal(rows)
	if err != nil || len(b) > maxManifest {
		return errCache
	}
	return atomicWrite(m.root, filepath.Join(m.root, "manifest.json"), b)
}

func (m *Manager) removeLocked(key string) {
	// Revocation of trust cannot depend on successfully unlinking the payload.
	delete(m.verified, key)
	// Failed deletion retains accounting and the manifest entry for later retry.
	if err := os.Remove(m.filename(key)); err != nil && !os.IsNotExist(err) {
		m.errorCode = "RESOURCE_CACHE_UNAVAILABLE"
		return
	}
	delete(m.entries, key)
	delete(m.refreshAfter, key)
}

func (m *Manager) usedLocked() int64 {
	var n int64
	for _, e := range m.entries {
		n += e.Size
	}
	return n
}

func (m *Manager) pruneLocked(incoming int64) bool {
	rows := make([]entry, 0, len(m.entries))
	for _, e := range m.entries {
		rows = append(rows, e)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Access < rows[j].Access })
	enough := func() bool {
		free, known := freeBytes(m.root)
		return m.usedLocked()+incoming <= m.policy.MaxBytes && len(m.entries) < maxEntries && (!known || free >= uint64(incoming+diskMargin))
	}
	for _, e := range rows {
		if enough() {
			return true
		}
		m.removeLocked(e.Key)
	}
	return enough()
}

// getLocked returns with mu held, but releases it during bounded disk reads and
// validation. Lifecycle and entry checks prevent stale reads from surviving a
// clear, policy change, close, removal or replacement while the lock is released.
func (m *Manager) getLocked(ctx context.Context, key, kind string) []byte {
	if ctx.Err() != nil || m.closed || m.ctx.Err() != nil || !m.policy.Enabled {
		return nil
	}
	e, ok := m.entries[key]
	if !ok || e.Kind != kind {
		return nil
	}
	gen := m.generation
	verified := m.verified[key]
	if kind == "cover" && (verified.digest != e.Digest || verified.mime == "") {
		pending := m.validating[key]
		if pending == nil || pending.generation != gen {
			workCtx, cancel := context.WithCancel(m.ctx)
			pending = &validation{done: make(chan struct{}), ctx: workCtx, cancel: cancel, generation: gen}
			m.validating[key] = pending
			m.wg.Add(1)
			go m.validateCover(e, pending)
		}
		m.mu.Unlock()
		select {
		case <-pending.done:
		case <-pending.ctx.Done():
		case <-ctx.Done():
		case <-m.ctx.Done():
		}
		m.mu.Lock()
		if gen != m.generation || !pending.valid {
			return nil
		}
		return m.getLocked(ctx, key, kind)
	}
	m.mu.Unlock()
	b, err := readBounded(m.filename(key), e.Size)
	valid := err == nil && int64(len(b)) == e.Size && digest(b) == e.Digest
	if valid && kind != "cover" {
		valid = validData(kind, b)
	}
	m.mu.Lock()
	current, exists := m.entries[key]
	if ctx.Err() != nil || m.closed || m.ctx.Err() != nil || !m.policy.Enabled || gen != m.generation || !exists || current.Digest != e.Digest || current.Created != e.Created || current.Size != e.Size || current.Kind != e.Kind {
		return nil
	}
	if !valid {
		m.removeLocked(key)
		_ = m.persistLocked()
		return nil
	}
	current.Access = m.now().UnixNano()
	m.entries[key] = current
	return b
}

// Lazy validation belongs to the manager, so canceling any one caller cannot
// abort another caller's shared decode or its wait for a bounded worker slot.
// Local first-use validation has its own capacity, so slow network downloads
// cannot delay an otherwise available cached cover.
func (m *Manager) validateCover(e entry, flight *validation) {
	defer crashreport.Fatal("resource-validate")
	defer m.wg.Done()
	defer flight.cancel()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.validating[e.Key] == flight {
			delete(m.validating, e.Key)
		}
		close(flight.done)
	}()
	select {
	case m.validationWorkers <- struct{}{}:
		defer func() { <-m.validationWorkers }()
	case <-flight.ctx.Done():
		return
	}
	// A canceled slot wait can race with admission. Recheck lifecycle and entry
	// identity before touching payloads, as well as before publishing results.
	m.mu.Lock()
	current := m.validationCurrentLocked(e, flight)
	m.mu.Unlock()
	if !current {
		return
	}
	var mime string
	var err error
	// Only payload reads and codecs are recoverable; cleanup stays outside.
	if crashreport.Contain("resource-validate", func() {
		b, readErr := readBounded(m.filename(e.Key), e.Size)
		err = readErr
		if err == nil && int64(len(b)) == e.Size && digest(b) == e.Digest {
			mime, err = coverMIME(b)
		}
	}) {
		mime, err = "", errCache
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.validationCurrentLocked(e, flight) {
		return
	}
	if err != nil || mime == "" {
		m.removeLocked(e.Key)
		_ = m.persistLocked()
		return
	}
	m.verified[e.Key] = verifiedCover{digest: e.Digest, mime: mime}
	flight.valid = true
}

func (m *Manager) validationCurrentLocked(e entry, flight *validation) bool {
	current, exists := m.entries[e.Key]
	return !m.closed && flight.ctx.Err() == nil && m.policy.Enabled && flight.generation == m.generation && exists && current.Digest == e.Digest && current.Created == e.Created && current.Size == e.Size && current.Kind == e.Kind
}

// Lifecycle changes abandon queued work immediately. Running image decoders
// drain naturally, but cannot publish into a later generation.
func (m *Manager) invalidateValidationsLocked() {
	for key, flight := range m.validating {
		flight.cancel()
		delete(m.validating, key)
	}
}

// run coalesces by opaque key. Each caller can cancel its own wait; downloads
// belong to the manager context and have a separate bounded lifetime.
func (m *Manager) run(ctx context.Context, key, kind string, fetch func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, errCache
	}
	initialGeneration := m.generation
	var immediate []byte
	if m.policy.Enabled {
		b := m.getLocked(ctx, key, kind)
		if err := ctx.Err(); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		if b != nil {
			e := m.entries[key]
			if kind != "lyric" || m.now().Sub(time.Unix(0, e.Created)) < lyricTTL {
				m.mu.Unlock()
				return b, nil
			}
			// Valid expired lyrics are returned immediately, even offline. Refresh
			// shares the bounded job machinery without owning the caller's context.
			immediate = b
			if m.now().Before(m.refreshAfter[key]) {
				m.mu.Unlock()
				return immediate, nil
			}
		}
	}
	// getLocked temporarily releases mu; a lifecycle transition may have
	// occurred while reading. A stale request cannot repopulate after clear.
	if m.closed || m.ctx.Err() != nil || initialGeneration != m.generation {
		m.mu.Unlock()
		return nil, errCache
	}
	j := m.jobs[key]
	if j == nil {
		// Reserve admission slots as well as execution capacity for lyrics, so a
		// long artwork queue cannot consume every foreground lyric slot.
		if len(m.jobs) >= 64 || (kind == "cover" && len(m.jobs) >= 60) {
			m.mu.Unlock()
			if immediate != nil {
				return immediate, nil
			}
			return nil, errCache
		}
		j = &job{done: make(chan struct{})}
		m.jobs[key] = j
		if immediate != nil {
			m.refreshAfter[key] = m.now().Add(5 * time.Minute)
		}
		gen := m.generation
		m.wg.Add(1)
		go func() {
			defer crashreport.Fatal("resource-fetch")
			defer m.wg.Done()
			workCtx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
			defer cancel()
			var b []byte
			var err error
			var mime string
			var valid bool
			workers := m.coverWorkers
			if kind == "lyric" {
				workers = m.lyricWorkers
			}
			select {
			case workers <- struct{}{}:
				if crashreport.Contain("resource-fetch", func() {
					defer func() { <-workers }()
					b, err = fetch(workCtx)
					// Decode outside the shared state mutex, within the worker slot.
					if err == nil {
						if kind == "cover" {
							mime, err = coverMIME(b)
							valid = err == nil
						} else {
							valid = validData(kind, b)
						}
					}
				}) {
					b, mime, valid, err = nil, "", false, errCache
				}
			case <-workCtx.Done():
				err = workCtx.Err()
			}
			if err == nil && workCtx.Err() != nil {
				err = workCtx.Err()
			}
			var contentDigest string
			if valid {
				contentDigest = digest(b)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if err == nil && gen == m.generation && !m.closed && m.ctx.Err() == nil && m.policy.Enabled && valid && m.pruneLocked(int64(len(b))) {
				if atomicWrite(m.root, m.filename(key), b) == nil {
					now := m.now().UnixNano()
					m.entries[key] = entry{Key: key, Kind: kind, Digest: contentDigest, Size: int64(len(b)), Created: now, Access: now}
					if kind == "cover" {
						m.verified[key] = verifiedCover{digest: m.entries[key].Digest, mime: mime}
					}
					if m.persistLocked() != nil {
						m.errorCode = "RESOURCE_CACHE_UNAVAILABLE"
					}
				} else {
					m.errorCode = "RESOURCE_CACHE_UNAVAILABLE"
				}
			}
			j.data = b
			j.err = err
			delete(m.jobs, key)
			close(j.done)
		}()
	}
	m.mu.Unlock()
	if immediate != nil {
		return immediate, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-j.done:
		return j.data, j.err
	}
}

func (m *Manager) Cover(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if !domain.ValidCover(ref) {
		return "", errInvalid
	}
	// Unsupported codecs retain the trusted original URL without caching bytes.
	lower := strings.ToLower(ref)
	if strings.HasSuffix(lower, ".webp") || strings.HasSuffix(lower, ".avif") {
		return ref, nil
	}
	m.mu.Lock()
	enabled := m.policy.Enabled && !m.closed
	m.mu.Unlock()
	if !enabled {
		return ref, nil
	}
	key := keyFor("cover", ref)
	_, err := m.run(ctx, key, "cover", func(ctx context.Context) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, errCache
		}
		// The caller's canonical reference has already passed the no-query
		// trusted-cover validator. Resize only the outgoing display-art request;
		// cache identity and fallback continue to use the original reference.
		query := req.URL.Query()
		query.Set("param", "1024y1024")
		req.URL.RawQuery = query.Encode()
		req.Header.Set("Accept", "image/jpeg,image/png,image/gif")
		resp, err := m.client.Do(req)
		if err != nil {
			return nil, errCache
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.ContentLength > maxCover {
			return nil, errInvalid
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxCover+1))
		if err != nil {
			return nil, errCache
		}
		return b, nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return ref, nil
	}
	m.mu.Lock()
	e, ok := m.entries[key]
	verified := m.verified[key]
	available := ok && e.Kind == "cover" && verified.digest == e.Digest && verified.mime != "" && m.policy.Enabled && !m.closed && m.ctx.Err() == nil
	m.mu.Unlock()
	if !available {
		return ref, nil
	}
	return "/artwork/" + key, nil
}

func (m *Manager) Lyrics(ctx context.Context, id string, fetch func(context.Context, string) (domain.LyricData, error)) (domain.LyricData, error) {
	if !domain.ValidID(id) || fetch == nil {
		return domain.LyricData{}, errInvalid
	}
	b, err := m.run(ctx, keyFor("lyric", id), "lyric", func(ctx context.Context) ([]byte, error) {
		v, err := fetch(ctx, id)
		if err != nil {
			return nil, err
		}
		return lyricBytes(v)
	})
	if err != nil {
		return domain.LyricData{}, err
	}
	var v domain.LyricData
	if json.Unmarshal(b, &v) != nil {
		return v, errInvalid
	}
	return v, nil
}

func (m *Manager) Stats() domain.ResourceCacheStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := domain.ResourceCacheStats{Available: !m.closed && m.ctx.Err() == nil, Enabled: m.policy.Enabled, MaxBytes: m.policy.MaxBytes, UsedBytes: m.usedLocked(), ErrorCode: m.errorCode}
	for _, e := range m.entries {
		if e.Kind == "cover" {
			s.CachedCovers++
		} else {
			s.CachedLyrics++
		}
	}
	return s
}

func (m *Manager) Clear() (domain.ResourceCacheClearResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result domain.ResourceCacheClearResult
	if m.closed {
		return result, errCache
	}
	m.generation++
	m.invalidateValidationsLocked()
	for key, e := range m.entries {
		m.removeLocked(key)
		if _, ok := m.entries[key]; !ok {
			if e.Kind == "cover" {
				result.RemovedCovers++
			} else {
				result.RemovedLyrics++
			}
		}
	}
	err := m.persistLocked()
	if len(m.entries) > 0 || err != nil {
		return result, errCache
	}
	m.errorCode = ""
	return result, nil
}

func (m *Manager) ApplySettings() {
	v := m.currentSettings()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if v != m.policy {
		m.generation++
		m.invalidateValidationsLocked()
		m.policy = v
		m.pruneLocked(0)
		if m.persistLocked() != nil {
			m.errorCode = "RESOURCE_CACHE_UNAVAILABLE"
		}
	}
}

func (m *Manager) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer crashreport.FatalHTTP("resource-handler")
		if !strings.HasPrefix(r.URL.Path, "/artwork/") {
			if next != nil {
				next.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/artwork/")
		if !hashPattern.MatchString(key) || r.URL.RawQuery != "" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		m.mu.Lock()
		var b []byte
		var mime string
		if !m.closed && m.ctx.Err() == nil && m.policy.Enabled && r.Context().Err() == nil {
			b = m.getLocked(r.Context(), key, "cover")
			if b != nil {
				mime = m.verified[key].mime
			}
		}
		m.mu.Unlock()
		if b == nil || mime == "" || r.Context().Err() != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "artwork", time.Time{}, bytes.NewReader(b))
	})
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.generation++
	m.invalidateValidationsLocked()
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.persistLocked()
	if m.unlock != nil {
		if e := m.unlock(); e != nil {
			err = e
		}
		m.unlock = nil
	}
	return err
}
