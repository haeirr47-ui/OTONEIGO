package resourcecache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

var probeDecodes atomic.Int32
var probeHook atomic.Pointer[func()]

func init() {
	image.RegisterFormat("png", "CACHEPROBE", func(r io.Reader) (image.Image, error) {
		probeDecodes.Add(1)
		if hook := probeHook.Load(); hook != nil {
			(*hook)()
		}
		if _, err := io.CopyN(io.Discard, r, 10); err != nil {
			return nil, err
		}
		return png.Decode(r)
	}, func(r io.Reader) (image.Config, error) {
		if _, err := io.CopyN(io.Discard, r, 10); err != nil {
			return image.Config{}, err
		}
		return png.DecodeConfig(r)
	})
}

func TestArtworkHitsReuseFullValidation(t *testing.T) {
	data := append([]byte("CACHEPROBE"), picture(t)...)
	m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
	probeDecodes.Store(0)
	local, err := m.Cover(context.Background(), testCover)
	if err != nil || local == testCover {
		t.Fatalf("admission: %q %v", local, err)
	}
	if got := probeDecodes.Load(); got != 1 {
		t.Fatalf("admission performed %d full decodes; want 1", got)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	probeDecodes.Store(0)
	m = manager(t, Options{Root: m.root})
	if got := probeDecodes.Load(); got != 0 {
		t.Fatalf("restart performed %d full decodes; want 0", got)
	}
	probeDecodes.Store(0)
	h := m.Handler(nil)
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, local, nil))
		if w.Code != 200 {
			t.Fatalf("hit: %d", w.Code)
		}
	}
	if got := probeDecodes.Load(); got != 1 {
		t.Fatalf("first GET plus warm HEAD performed %d full image decodes; want 1", got)
	}
}

// Seed the persisted boundary directly: startup must retain metadata even when
// bytes have not yet earned trust. In particular, disabled startup must not
// hash, decode, or discard payloads on the UI construction path.
func TestStartupDefersPayloadValidation(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			root := t.TempDir()
			var rows []entry
			for i := 0; i < 24; i++ {
				data := append([]byte("CACHEPROBE"), picture(t)...)
				kind := "cover"
				if i == 23 {
					kind = "lyric"
					data = []byte("invalid lyric")
				}
				key := keyFor(kind, fmt.Sprint(i))
				e := entry{Key: key, Kind: kind, Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1}
				// A wrong digest is deliberately accepted only as unverified metadata.
				if i == 22 {
					e.Digest = digest([]byte("different bytes"))
				}
				rows = append(rows, e)
				if err := os.WriteFile(filepath.Join(root, key+".bin"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			manifest, _ := json.Marshal(rows)
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			policy := domain.DefaultResourceCacheSettings()
			policy.Enabled = enabled
			probeDecodes.Store(0)
			m := manager(t, Options{Root: root, Settings: func() domain.ResourceCacheSettings { return policy }})
			if got := probeDecodes.Load(); got != 0 {
				t.Fatalf("startup decoded %d covers", got)
			}
			if len(m.entries) != len(rows) || len(m.verified) != 0 {
				t.Fatalf("startup eagerly validated/discarded metadata: %d entries, %d verified", len(m.entries), len(m.verified))
			}
			for _, row := range rows {
				if _, err := os.Stat(m.filename(row.Key)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkArtworkHandlerWarm1024PNG(b *testing.B) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1024, 1024))); err != nil {
		b.Fatal(err)
	}
	data := encoded.Bytes()
	m, err := New(Options{Root: b.TempDir(), HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	local, err := m.Cover(context.Background(), testCover)
	if err != nil || local == testCover {
		b.Fatalf("admission %s %v", local, err)
	}
	h := m.Handler(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", local, nil))
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}

// A real image decoder is held in the middle of admission. Foreground state,
// lyrics and clear must all complete before the expensive decode can finish.
func TestArtworkDecodeDoesNotHoldStateLockAndClearInvalidates(t *testing.T) {
	data := append([]byte("CACHEPROBE"), picture(t)...)
	m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
	started := make(chan struct{})
	release := make(chan struct{})
	hook := func() { close(started); <-release }
	probeHook.Store(&hook)
	defer probeHook.Store(nil)
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	done := make(chan string, 1)
	go func() { local, _ := m.Cover(context.Background(), testCover); done <- local }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("decode not started")
	}
	foreground := make(chan error, 1)
	go func() {
		m.Stats()
		_, err := m.Lyrics(context.Background(), "123", func(context.Context, string) (domain.LyricData, error) {
			return domain.LyricData{Original: "foreground"}, nil
		})
		if err == nil {
			_, err = m.Clear()
		}
		foreground <- err
	}()
	select {
	case err := <-foreground:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("decode blocked state, lyrics or clear")
	}
	close(release)
	released = true
	if local := <-done; local != testCover {
		t.Fatalf("decode admitted after clear: %s", local)
	}
	if stats := m.Stats(); stats.CachedCovers != 0 || stats.CachedLyrics != 0 {
		t.Fatalf("late entries: %+v", stats)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.verified) != 0 {
		t.Fatal("clear retained verified metadata")
	}
}

func TestArtworkVerifiedDigestRejectsSameLengthCorruption(t *testing.T) {
	data := picture(t)
	m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
	local, _ := m.Cover(context.Background(), testCover)
	key := strings.TrimPrefix(local, "/artwork/")
	// Same byte count defeats size-only checks while preserving the old digest
	// and in-memory validation record. Neither GET nor HEAD may trust that record.
	corrupt := append([]byte(nil), data...)
	corrupt[len(corrupt)/2] ^= 0xff
	if err := os.WriteFile(m.filename(key), corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		m.Handler(nil).ServeHTTP(w, httptest.NewRequest(method, local, nil))
		if w.Code != 404 {
			t.Fatalf("corrupt %s returned %d", method, w.Code)
		}
	}
	if m.Stats().CachedCovers != 0 {
		t.Fatal("corrupt entry retained")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.verified) != 0 {
		t.Fatal("corrupt metadata retained")
	}
}

func TestArtworkSameSourceAfterClearUsesNewValidation(t *testing.T) {
	first := picture(t)
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 3, 3)), nil); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return response(r, 200, first), nil
		}
		return response(r, 200, jpegBytes.Bytes()), nil
	})}})
	local, _ := m.Cover(context.Background(), testCover)
	if _, err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := m.Cover(context.Background(), testCover)
	if refreshed != local {
		t.Fatal("source hash identity unexpectedly changed")
	}
	w := httptest.NewRecorder()
	m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", local, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), jpegBytes.Bytes()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("stale artwork bytes/MIME/cache policy after clear: %d %v", w.Code, w.Header())
	}
}

func TestLazyArtworkValidationCoalescesAndInvalidates(t *testing.T) {
	for _, transition := range []string{"clear", "disable", "close", "none"} {
		t.Run(transition, func(t *testing.T) {
			data := append([]byte("CACHEPROBE"), picture(t)...)
			first := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
			local, _ := first.Cover(context.Background(), testCover)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			policy := domain.DefaultResourceCacheSettings()
			m := manager(t, Options{Root: first.root, Settings: func() domain.ResourceCacheSettings { return policy }})
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			hook := func() { once.Do(func() { close(started) }); <-release }
			probeDecodes.Store(0)
			probeHook.Store(&hook)
			defer probeHook.Store(nil)
			defer onceRelease(release)()
			results := make(chan int, 12)
			for i := 0; i < 12; i++ {
				go func() {
					w := httptest.NewRecorder()
					m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", local, nil))
					results <- w.Code
				}()
			}
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("lazy decode never started")
			}
			foreground := make(chan error, 1)
			closeDone := make(chan error, 1)
			go func() {
				m.Stats()
				_, err := m.Lyrics(context.Background(), "123", func(context.Context, string) (domain.LyricData, error) {
					return domain.LyricData{Original: "foreground"}, nil
				})
				if err == nil {
					switch transition {
					case "clear":
						_, err = m.Clear()
					case "disable":
						policy.Enabled = false
						m.ApplySettings()
					case "close":
						go func() { closeDone <- m.Close() }()
						<-m.ctx.Done()
					}
				}
				foreground <- err
			}()
			select {
			case err := <-foreground:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("lazy decode held shared state lock")
			}
			close(release)
			for i := 0; i < 12; i++ {
				select {
				case code := <-results:
					want := 200
					if transition != "none" {
						want = 404
					}
					if code != want {
						t.Fatalf("response=%d want=%d", code, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("validation follower blocked")
				}
			}
			if got := probeDecodes.Load(); got != 1 {
				t.Fatalf("concurrent first hits decoded %d times", got)
			}
			if transition == "close" {
				select {
				case err := <-closeDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("close did not drain lazy validation")
				}
			}
			if transition != "none" && len(m.verified) != 0 {
				t.Fatal("stale validation survived lifecycle transition")
			}
		})
	}
}

// Allows failure paths to release a blocked decoder before manager cleanup.
func onceRelease(ch chan struct{}) func() {
	return func() {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}

func BenchmarkStartupMetadataOnly(b *testing.B) {
	for _, size := range []int{32, 2048} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			root := b.TempDir()
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
				b.Fatal(err)
			}
			data := encoded.Bytes()
			var rows []entry
			for i := 0; i < 64; i++ {
				key := keyFor("cover", fmt.Sprint(i))
				if err := os.WriteFile(filepath.Join(root, key+".bin"), data, 0600); err != nil {
					b.Fatal(err)
				}
				rows = append(rows, entry{Key: key, Kind: "cover", Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1})
			}
			manifest, _ := json.Marshal(rows)
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m, err := New(Options{Root: root})
				if err != nil {
					b.Fatal(err)
				}
				if err = m.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestLazyArtworkRejectsIncompleteImageBeforeServing(t *testing.T) {
	root := t.TempDir()
	// Valid dimensions and a matching manifest digest do not prove complete pixels.
	data := append([]byte("CACHEPROBE"), picture(t)[:33]...)
	key := keyFor("cover", "truncated")
	row := entry{Key: key, Kind: "cover", Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1}
	manifest, _ := json.Marshal([]entry{row})
	if err := os.WriteFile(filepath.Join(root, key+".bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	probeDecodes.Store(0)
	m := manager(t, Options{Root: root})
	if len(m.entries) != 1 || probeDecodes.Load() != 0 {
		t.Fatal("payload was validated during startup")
	}
	w := httptest.NewRecorder()
	m.Handler(nil).ServeHTTP(w, httptest.NewRequest("HEAD", "/artwork/"+key, nil))
	if w.Code != 404 || len(m.entries) != 0 || len(m.verified) != 0 || probeDecodes.Load() != 1 {
		t.Fatalf("incomplete image escaped full validation: status=%d entries=%d verified=%d decodes=%d", w.Code, len(m.entries), len(m.verified), probeDecodes.Load())
	}
}

func TestLazyArtworkValidationUsesBoundedWorkers(t *testing.T) {
	root := t.TempDir()
	data := append([]byte("CACHEPROBE"), picture(t)...)
	var rows []entry
	for i := 0; i < 8; i++ {
		key := keyFor("cover", fmt.Sprint(i))
		rows = append(rows, entry{Key: key, Kind: "cover", Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1})
		if err := os.WriteFile(filepath.Join(root, key+".bin"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, _ := json.Marshal(rows)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	m := manager(t, Options{Root: root})
	entered, release := make(chan struct{}, 8), make(chan struct{})
	hook := func() { entered <- struct{}{}; <-release }
	probeHook.Store(&hook)
	defer probeHook.Store(nil)
	defer onceRelease(release)()
	results := make(chan int, 8)
	for _, row := range rows {
		go func(key string) {
			w := httptest.NewRecorder()
			m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", "/artwork/"+key, nil))
			results <- w.Code
		}(row.Key)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	// All remaining first uses must be waiting at the separate two-slot validation gate.
	m.mu.Lock()
	active := len(m.validating)
	m.mu.Unlock()
	if active < 2 || len(m.validationWorkers) != 2 {
		t.Fatalf("invalid worker admission: active=%d workers=%d", active, len(m.validationWorkers))
	}
	select {
	case <-entered:
		t.Fatal("more than two lazy image decoders ran together")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for i := 0; i < 8; i++ {
		select {
		case code := <-results:
			if code != 200 {
				t.Fatal(code)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not complete")
		}
	}
}

type cancellationProbeContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *cancellationProbeContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestLazyArtworkWaiterCancellation(t *testing.T) {
	data := append([]byte("CACHEPROBE"), picture(t)...)
	first := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
	local, _ := first.Cover(context.Background(), testCover)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	m := manager(t, Options{Root: first.root})
	started, release := make(chan struct{}), make(chan struct{})
	hook := func() { close(started); <-release }
	probeHook.Store(&hook)
	defer probeHook.Store(nil)
	defer onceRelease(release)()
	leader := make(chan string, 1)
	go func() { url, _ := m.Cover(context.Background(), testCover); leader <- url }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("leader decode did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &cancellationProbeContext{Context: ctx, entered: make(chan struct{})}
	follower := make(chan error, 1)
	go func() { _, err := m.Cover(observed, testCover); follower <- err }()
	<-observed.entered
	cancel()
	select {
	case err := <-follower:
		if err != context.Canceled {
			t.Fatalf("canceled waiter: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled waiter remained blocked behind shared decoder")
	}
	close(release)
	select {
	case got := <-leader:
		if got != local {
			t.Fatalf("canceled follower discarded shared validation: %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("leader remained blocked")
	}
}

func TestLazyArtworkWorkerWaitCancellation(t *testing.T) {
	data := append([]byte("CACHEPROBE"), picture(t)...)
	first := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
	local, _ := first.Cover(context.Background(), testCover)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	m := manager(t, Options{Root: first.root})
	// Occupy the local validation gate with other lazy decodes.
	for i := 0; i < cap(m.validationWorkers); i++ {
		m.validationWorkers <- struct{}{}
	}
	release := func() {
		for i := 0; i < cap(m.validationWorkers); i++ {
			<-m.validationWorkers
		}
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	probeDecodes.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &cancellationProbeContext{Context: ctx, entered: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, err := m.Cover(observed, testCover); result <- err }()
	select {
	case <-observed.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cover did not wait for validation")
	}
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("worker waiter: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled first caller remained blocked waiting for a worker")
	}
	// An HTTP follower must independently cancel while shared work is queued.
	httpCtx, cancelHTTP := context.WithCancel(context.Background())
	defer cancelHTTP()
	httpObserved := &cancellationProbeContext{Context: httpCtx, entered: make(chan struct{})}
	httpResult := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", local, nil).WithContext(httpObserved))
		httpResult <- w.Code
	}()
	select {
	case <-httpObserved.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP follower did not wait for validation")
	}
	cancelHTTP()
	select {
	case code := <-httpResult:
		if code != http.StatusNotFound {
			t.Fatalf("canceled HTTP waiter: %d", code)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled HTTP follower remained blocked waiting for validation")
	}
	m.mu.Lock()
	pending := m.validating[keyFor("cover", testCover)]
	m.mu.Unlock()
	if pending == nil || probeDecodes.Load() != 0 {
		t.Fatal("caller cancellation discarded shared validation or bypassed worker capacity")
	}
	// The queued validation remains useful to a later live caller.
	survivor := make(chan string, 1)
	go func() { url, _ := m.Cover(context.Background(), testCover); survivor <- url }()
	release()
	release = nil
	select {
	case got := <-survivor:
		if got != local || probeDecodes.Load() != 1 {
			t.Fatalf("shared validation lost: url=%s decodes=%d", got, probeDecodes.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live caller did not receive shared validation")
	}
}

func TestLazyArtworkQueuedValidationInvalidates(t *testing.T) {
	for _, transition := range []string{"clear", "settings"} {
		t.Run(transition, func(t *testing.T) {
			data := append([]byte("CACHEPROBE"), picture(t)...)
			first := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return response(r, 200, data), nil })}})
			local, _ := first.Cover(context.Background(), testCover)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			policy := domain.DefaultResourceCacheSettings()
			m := manager(t, Options{Root: first.root, Settings: func() domain.ResourceCacheSettings { return policy }})
			for i := 0; i < cap(m.validationWorkers); i++ {
				m.validationWorkers <- struct{}{}
			}
			defer func() {
				for i := 0; i < cap(m.validationWorkers); i++ {
					<-m.validationWorkers
				}
			}()
			probeDecodes.Store(0)
			cycles := 1
			if transition == "settings" {
				cycles = 4
			}
			for i := 0; i < cycles; i++ {
				observed := &cancellationProbeContext{Context: context.Background(), entered: make(chan struct{})}
				done := make(chan int, 1)
				go func() {
					w := httptest.NewRecorder()
					m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", local, nil).WithContext(observed))
					done <- w.Code
				}()
				select {
				case <-observed.entered:
				case <-time.After(2 * time.Second):
					t.Fatal("validation was not queued")
				}
				m.mu.Lock()
				pending := m.validating[keyFor("cover", testCover)]
				m.mu.Unlock()
				if pending == nil {
					t.Fatal("missing queued validation")
				}
				if transition == "clear" {
					if _, err := m.Clear(); err != nil {
						t.Fatal(err)
					}
				} else {
					policy.Enabled = false
					m.ApplySettings()
					policy.Enabled = true
					m.ApplySettings()
				}
				select {
				case <-pending.done:
				case <-time.After(300 * time.Millisecond):
					t.Fatal("obsolete validation remained queued behind occupied workers")
				}
				select {
				case code := <-done:
					if code != http.StatusNotFound {
						t.Fatalf("obsolete request returned %d", code)
					}
				case <-time.After(300 * time.Millisecond):
					t.Fatal("obsolete caller remained queued")
				}
			}
			m.wg.Wait()
			if got := probeDecodes.Load(); got != 0 {
				t.Fatalf("obsolete queued work decoded %d times", got)
			}
		})
	}
}
func TestLazyArtworkValidationIndependentOfNetworkWorkers(t *testing.T) {
	for _, caller := range []string{"cover", "handler"} {
		t.Run(caller, func(t *testing.T) {
			root := t.TempDir()
			imageData := picture(t)
			data := append([]byte("CACHEPROBE"), imageData...)
			key := keyFor("cover", testCover)
			row := entry{Key: key, Kind: "cover", Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1}
			manifest, err := json.Marshal([]entry{row})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, key+".bin"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}, 4), make(chan struct{})
			var networkCalls atomic.Int32
			m := manager(t, Options{Root: root, HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				networkCalls.Add(1)
				entered <- struct{}{}
				select {
				case <-release:
					return response(r, http.StatusOK, imageData), nil
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			})}})
			defer onceRelease(release)()
			probeDecodes.Store(0)
			if len(m.verified) != 0 {
				t.Fatal("startup trusted persisted cover without validation")
			}
			for i := 0; i < 4; i++ {
				go func(i int) {
					_, _ = m.Cover(context.Background(), fmt.Sprintf("https://p1.music.126.net/blocked-%d.jpg", i))
				}(i)
			}
			for i := 0; i < 4; i++ {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("four network fetches did not start")
				}
			}
			if len(m.coverWorkers) != 4 {
				t.Fatal("network fetches did not occupy all four slots")
			}
			done := make(chan error, 1)
			go func() {
				if caller == "cover" {
					local, err := m.Cover(context.Background(), testCover)
					if err == nil && local != "/artwork/"+key {
						err = fmt.Errorf("cached Cover returned %q", local)
					}
					done <- err
					return
				}
				w := httptest.NewRecorder()
				m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", "/artwork/"+key, nil))
				if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Content-Type") != "image/png" {
					done <- fmt.Errorf("cached handler: status=%d content-type=%q", w.Code, w.Header().Get("Content-Type"))
					return
				}
				done <- nil
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("cached first-use validation blocked behind four network fetches")
			}
			if probeDecodes.Load() != 1 || networkCalls.Load() != 4 || len(m.coverWorkers) != 4 {
				t.Fatalf("first use must fully decode without waiting for or adding network requests: decodes=%d requests=%d busy=%d", probeDecodes.Load(), networkCalls.Load(), len(m.coverWorkers))
			}
		})
	}
}
