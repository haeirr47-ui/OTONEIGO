package resourcecache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

const testCover = "https://p1.music.126.net/public-cover.jpg"

type roundTrip func(*http.Request) (*http.Response, error)

type observedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func picture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func response(r *http.Request, status int, b []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b)), Request: r}
}

func manager(t *testing.T, o Options) *Manager {
	t.Helper()
	if o.Root == "" {
		o.Root = t.TempDir()
	}
	m, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestCoverAssetPersistenceAndCorruption(t *testing.T) {
	img := picture(t)
	var calls atomic.Int32
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "p1.music.126.net" || r.URL.Path != "/public-cover.jpg" || r.URL.User != nil || r.URL.Fragment != "" || r.URL.RawQuery != "param=1024y1024" {
			t.Errorf("unexpected resized artwork request: %v", r.URL)
		}
		return response(r, 200, img), nil
	})}
	root := t.TempDir()
	m := manager(t, Options{Root: root, HTTPClient: client})
	local, err := m.Cover(context.Background(), testCover)
	if err != nil || !strings.HasPrefix(local, "/artwork/") {
		t.Fatalf("cover: %q %v", local, err)
	}
	if local != "/artwork/"+keyFor("cover", testCover) {
		t.Fatal("resize changed canonical cache identity")
	}
	h := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", local, nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), img) || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/index.html", nil))
	if w.Code != 204 {
		t.Fatal("next handler not called")
	}
	for _, path := range []string{"/artwork/../../manifest.json", local + "?x=y", "/artwork/" + strings.Repeat("A", 64), local + "/x"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("invalid route accepted: %s", path)
		}
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte("music.126.net")) || bytes.Contains(manifest, []byte("public-cover")) || bytes.Contains(manifest, []byte("1024y1024")) {
		t.Fatal("manifest contains source reference")
	}
	m = manager(t, Options{Root: root, HTTPClient: client})
	if got, _ := m.Cover(context.Background(), testCover); got != local || calls.Load() != 1 {
		t.Fatal("restart did not hit persistent cache")
	}
	key := strings.TrimPrefix(local, "/artwork/")
	if err = os.WriteFile(m.filename(key), []byte("<html>not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Cover(context.Background(), testCover); got != local || calls.Load() != 2 {
		t.Fatal("corrupt hit was not refetched")
	}
	stats := m.Stats()
	if stats.CachedCovers != 1 || stats.UsedBytes != int64(len(img)) {
		t.Fatalf("stats: %+v", stats)
	}
	result, err := m.Clear()
	if err != nil || result.RemovedCovers != 1 {
		t.Fatalf("clear: %+v %v", result, err)
	}
	w = httptest.NewRecorder()
	h = m.Handler(nil)
	h.ServeHTTP(w, httptest.NewRequest("GET", local, nil))
	if w.Code != 404 {
		t.Fatal("cleared asset still served")
	}
}

func TestCoverSecurityAndUnsupportedBypass(t *testing.T) {
	var calls atomic.Int32
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(testCover)
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "secret"}})
	client := &http.Client{Jar: jar, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials leaked")
		}
		resp := response(r, 302, nil)
		resp.Header.Set("Location", "https://evil.example/image.png")
		return resp, nil
	})}
	m := manager(t, Options{HTTPClient: client})
	for _, ref := range []string{"http://p1.music.126.net/a.jpg", "https://p1.music.126.net:443/a.jpg", "https://p1.music.126.net/a.jpg?token=secret", "https://p1.music.126.net/a.jpg?param=1024y1024", "https://u:p@p1.music.126.net/a.jpg", "https://evil.example/a.jpg", "https://p1.music.126.net/a.svg", "/artwork/abc"} {
		if _, err := m.Cover(context.Background(), ref); err == nil {
			t.Fatalf("untrusted ref accepted: %s", ref)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("untrusted network request")
	}
	if got, err := m.Cover(context.Background(), ""); got != "" || err != nil {
		t.Fatal("empty cover")
	}
	for _, ext := range []string{"webp", "avif"} {
		ref := "https://p1.music.126.net/a." + ext
		if got, err := m.Cover(context.Background(), ref); got != ref || err != nil {
			t.Fatal("unsupported codec not bypassed")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported codec fetched")
	}
	if got, err := m.Cover(context.Background(), testCover); got != testCover || err != nil {
		t.Fatal("failed fetch should return original")
	}
	if calls.Load() != 1 || m.Stats().CachedCovers != 0 {
		t.Fatal("redirect followed or cached")
	}
}

func TestIgnoredArtworkResizeStillEnforcesSizeLimit(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), maxCover+1)
	for _, unknownLength := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown-length=%t", unknownLength), func(t *testing.T) {
			var fetched atomic.Bool
			m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				fetched.Store(true)
				if r.URL.RawQuery != "param=1024y1024" {
					t.Error("missing fixed artwork resize")
				}
				resp := response(r, 200, oversized)
				if unknownLength {
					resp.ContentLength = -1
				}
				return resp, nil
			})}})
			got, err := m.Cover(context.Background(), testCover)
			if err != nil || got != testCover || !fetched.Load() || m.Stats().CachedCovers != 0 {
				t.Fatalf("oversized resize response changed fallback or cached: %q %v", got, err)
			}
		})
	}
}

func TestRejectInvalidImageBodies(t *testing.T) {
	large := bytes.Repeat([]byte("x"), maxCover+1)
	for name, b := range map[string][]byte{"html": []byte("<html>oops</html>"), "svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "large": large, "empty": nil} {
		t.Run(name, func(t *testing.T) {
			m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				resp := response(r, 200, b)
				resp.Header.Set("Content-Type", "image/jpeg")
				return resp, nil
			})}})
			if got, err := m.Cover(context.Background(), testCover); got != testCover || err != nil || m.Stats().CachedCovers != 0 {
				t.Fatal("invalid image cached")
			}
		})
	}
	// The image header decoder must reject excessive dimensions without allocating pixels.
	img := picture(t)
	img[16] = 0x7f
	img[17] = 0xff
	img[18] = 0xff
	img[19] = 0xff
	if _, err := coverMIME(img); err == nil {
		t.Fatal("invalid dimensions accepted")
	}
}

func TestRejectTruncatedImagesWithValidConfiguration(t *testing.T) {
	pngBytes := picture(t)
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	jpegBytes := jpg.Bytes()
	// PNG's complete IHDR and JPEG's complete configuration both pass the
	// inexpensive header decoder, despite missing the remaining image data.
	var jpegHeader []byte
	for n := 1; n < len(jpegBytes); n++ {
		if _, _, err := image.DecodeConfig(bytes.NewReader(jpegBytes[:n])); err == nil {
			jpegHeader = jpegBytes[:n]
			break
		}
	}
	if jpegHeader == nil {
		t.Fatal("JPEG fixture has no header-only prefix")
	}
	for name, b := range map[string][]byte{"png-ihdr": pngBytes[:33], "png-missing-iend": pngBytes[:len(pngBytes)-12], "jpeg-header": jpegHeader, "jpeg-truncated": jpegBytes[:len(jpegBytes)-8]} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil {
				t.Fatalf("fixture does not reproduce header-only acceptance: %v", err)
			}
			m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, b), nil })}})
			if got, err := m.Cover(context.Background(), testCover); err != nil || got != testCover || m.Stats().CachedCovers != 0 {
				t.Fatalf("truncated image cached: %q %v", got, err)
			}
		})
	}
}

func TestSlowArtworkDoesNotBlockForegroundLyrics(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%t", disabled), func(t *testing.T) {
			policy := domain.DefaultResourceCacheSettings()
			started := make(chan struct{}, 4)
			release := make(chan struct{})
			m := manager(t, Options{Settings: func() domain.ResourceCacheSettings { return policy }, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				started <- struct{}{}
				select {
				case <-release:
					return nil, errors.New("offline")
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			})}})
			t.Cleanup(func() { close(release) })
			for i := 0; i < 4; i++ {
				go func(i int) {
					_, _ = m.Cover(context.Background(), fmt.Sprintf("https://p1.music.126.net/slow-%d.jpg", i))
				}(i)
			}
			for i := 0; i < 4; i++ {
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatal("artwork workers did not start")
				}
			}
			if disabled {
				policy.Enabled = false
				m.ApplySettings()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var fetched atomic.Bool
			got, err := m.Lyrics(ctx, "123", func(context.Context, string) (domain.LyricData, error) {
				fetched.Store(true)
				return domain.LyricData{Original: "foreground"}, nil
			})
			if err != nil || got.Original != "foreground" || !fetched.Load() {
				t.Fatalf("lyrics blocked behind artwork: %+v %v", got, err)
			}
			if disabled && m.Stats().CachedLyrics != 0 {
				t.Fatal("disabled lyric fetch persisted")
			}
		})
	}
}

func TestLyricsTTLAndOfflineRestart(t *testing.T) {
	now := time.Now()
	var calls atomic.Int32
	fetch := func(context.Context, string) (domain.LyricData, error) {
		calls.Add(1)
		return domain.LyricData{Original: "[00:01]公开歌词", Translated: "translation"}, nil
	}
	root := t.TempDir()
	m := manager(t, Options{Root: root, Now: func() time.Time { return now }})
	want, err := m.Lyrics(context.Background(), "12345678901234567890", fetch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Lyrics(context.Background(), "12345678901234567890", fetch); err != nil || calls.Load() != 1 {
		t.Fatal("lyric cache miss")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m = manager(t, Options{Root: root, Now: func() time.Time { return now }})
	refreshed := make(chan struct{})
	release := make(chan struct{})
	offline := func(context.Context, string) (domain.LyricData, error) {
		calls.Add(1)
		close(refreshed)
		<-release
		return domain.LyricData{}, errors.New("offline")
	}
	if got, err := m.Lyrics(context.Background(), "12345678901234567890", offline); got != want || err != nil || calls.Load() != 1 {
		t.Fatal("offline restart failed")
	}
	now = now.Add(8 * 24 * time.Hour)
	if got, err := m.Lyrics(context.Background(), "12345678901234567890", offline); got != want || err != nil {
		t.Fatal("stale offline fallback failed")
	}
	<-refreshed
	close(release)
	if calls.Load() != 2 {
		t.Fatal("stale refresh not attempted")
	}
	// Repeated renders do not hammer a failing public endpoint.
	for i := 0; i < 3; i++ {
		if got, err := m.Lyrics(context.Background(), "12345678901234567890", offline); got != want || err != nil {
			t.Fatal("stale retry failed")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("refresh retry cooldown ignored")
	}
	if _, err = m.Lyrics(context.Background(), "bad", fetch); err == nil {
		t.Fatal("invalid id accepted")
	}
	if m.Stats().CachedLyrics != 1 {
		t.Fatal("lyric count")
	}
}

func TestLyricsRejectOversizeInvalidUTF8AndDoNotCacheEmpty(t *testing.T) {
	m := manager(t, Options{})
	for _, v := range []domain.LyricData{{Original: string([]byte{0xff})}, {Original: strings.Repeat("a", maxLyric+1)}} {
		if _, err := m.Lyrics(context.Background(), "1", func(context.Context, string) (domain.LyricData, error) { return v, nil }); err == nil {
			t.Fatal("invalid lyrics accepted")
		}
	}
	var calls int
	for i := 0; i < 2; i++ {
		if _, err := m.Lyrics(context.Background(), "1", func(context.Context, string) (domain.LyricData, error) { calls++; return domain.LyricData{}, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || m.Stats().CachedLyrics != 0 {
		t.Fatal("empty lyrics cached")
	}
}

func TestCoalescingCancellationAndClearInFlight(t *testing.T) {
	img := picture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return response(r, 200, img), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}})
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := m.Cover(ctx, testCover); first <- err }()
	<-started
	second := make(chan string, 1)
	secondCtx := &observedContext{Context: context.Background(), observed: make(chan struct{})}
	go func() { ref, _ := m.Cover(secondCtx, testCover); second <- ref }()
	<-secondCtx.observed
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation: %v", err)
	}
	if _, err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got := <-second; got != testCover {
		t.Fatalf("late clear write: %s", got)
	}
	if calls.Load() != 1 || m.Stats().CachedCovers != 0 {
		t.Fatal("coalescing or generation invalidation failed")
	}
}

func TestDisablePreventsLateWriteAndRetainsExistingEntries(t *testing.T) {
	policy := domain.DefaultResourceCacheSettings()
	started := make(chan struct{})
	release := make(chan struct{})
	m := manager(t, Options{Settings: func() domain.ResourceCacheSettings { return policy }})
	done := make(chan error, 1)
	go func() {
		_, err := m.Lyrics(context.Background(), "1", func(context.Context, string) (domain.LyricData, error) {
			close(started)
			<-release
			return domain.LyricData{Original: "public"}, nil
		})
		done <- err
	}()
	<-started
	policy.Enabled = false
	m.ApplySettings()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.Stats().CachedLyrics != 0 || m.Stats().Enabled {
		t.Fatal("disabled cache populated")
	}
	policy.Enabled = true
	m.ApplySettings()
	if _, err := m.Lyrics(context.Background(), "2", func(context.Context, string) (domain.LyricData, error) {
		return domain.LyricData{Original: "public"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	policy.Enabled = false
	m.ApplySettings()
	if m.Stats().CachedLyrics != 1 {
		t.Fatal("disable erased retained cache")
	}
}

func TestLockAndCloseCancelOwner(t *testing.T) {
	root := t.TempDir()
	m := manager(t, Options{Root: root})
	if _, err := New(Options{Root: root}); !errors.Is(err, ErrInUse) {
		t.Fatalf("second owner: %v", err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := m.Lyrics(context.Background(), "1", func(ctx context.Context, _ string) (domain.LyricData, error) {
			close(started)
			<-ctx.Done()
			return domain.LyricData{}, ctx.Err()
		})
		done <- err
	}()
	<-started
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("close cancellation: %v", err)
	}
	next := manager(t, Options{Root: root})
	if next.Stats().CachedLyrics != 0 {
		t.Fatal("closed job committed")
	}
}

func TestOrphansCorruptManifestAndLRUQuota(t *testing.T) {
	root := t.TempDir()
	key := keyFor("cover", "orphan")
	if err := os.WriteFile(filepath.Join(root, key+".bin"), []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".tmp-leftover"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`[{"key":"../../outside","kind":"cover","size":1}]`), 0600); err != nil {
		t.Fatal(err)
	}
	m := manager(t, Options{Root: root})
	if _, err := os.Stat(filepath.Join(root, key+".bin")); !os.IsNotExist(err) {
		t.Fatal("orphan retained")
	}
	if _, err := os.Stat(filepath.Join(root, ".tmp-leftover")); !os.IsNotExist(err) {
		t.Fatal("temp retained")
	}
	// Exercise quota using synthetic accounting instead of downloading 64 MiB fixtures.
	m.mu.Lock()
	for i, k := range []string{keyFor("cover", "old"), keyFor("cover", "new")} {
		if err := os.WriteFile(m.filename(k), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		m.entries[k] = entry{Key: k, Kind: "cover", Size: 40 * 1024 * 1024, Access: int64(i + 1)}
	}
	m.policy.MaxBytes = 64 * 1024 * 1024
	if !m.pruneLocked(1024) || len(m.entries) != 1 || m.entries[keyFor("cover", "new")].Key == "" {
		t.Fatal("LRU quota pruning failed")
	}
	m.mu.Unlock()
}
