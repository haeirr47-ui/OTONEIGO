//go:build windows

package resourcecache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
	"golang.org/x/sys/windows"
)

// Deny actual payload reads at the OS boundary while leaving stat available.
// This catches a startup hash/read even if nobody invokes an image decoder.
func TestStartupNeverReadsPayloadWindows(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			root := t.TempDir()
			var rows []entry
			for _, kind := range []string{"cover", "lyric"} {
				data := []byte("unverified payload")
				key := keyFor(kind, "read-gate")
				path := filepath.Join(root, key+".bin")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				f, err := os.OpenFile(path, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				var overlapped windows.Overlapped
				if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, uint32(len(data)), 0, &overlapped); err != nil {
					f.Close()
					t.Fatal(err)
				}
				defer f.Close()
				if _, err := readBounded(path, maxCover); err == nil {
					t.Fatal("payload read gate is not active")
				}
				rows = append(rows, entry{Key: key, Kind: kind, Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1})
			}
			manifest, _ := json.Marshal(rows)
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			policy := domain.DefaultResourceCacheSettings()
			policy.Enabled = enabled
			m := manager(t, Options{Root: root, Settings: func() domain.ResourceCacheSettings { return policy }})
			if len(m.entries) != len(rows) {
				t.Fatalf("startup attempted gated reads and discarded records: %d", len(m.entries))
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// os.Open on Windows denies delete sharing. A matching digest still cannot
// turn a truncated image into trusted pixels, even when cleanup cannot unlink it.
func TestLazyArtworkLockedCorruptionReturnsWindows(t *testing.T) {
	for _, endpoint := range []string{"cover", "handler"} {
		t.Run(endpoint, func(t *testing.T) {
			root := t.TempDir()
			data := append([]byte("CACHEPROBE"), picture(t)[:33]...)
			key := keyFor("cover", testCover)
			path := filepath.Join(root, key+".bin")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			manifest, _ := json.Marshal([]entry{{Key: key, Kind: "cover", Digest: digest(data), Size: int64(len(data)), Created: 1, Access: 1}})
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			locked, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer locked.Close()
			if err := os.Remove(path); err == nil {
				t.Fatal("file did not deny deletion")
			}
			m := manager(t, Options{Root: root, HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("offline")
			})}})
			probeDecodes.Store(0)
			done := make(chan struct{})
			go func() {
				defer close(done)
				if endpoint == "cover" {
					got, err := m.Cover(context.Background(), testCover)
					if err != nil || got != testCover {
						t.Errorf("corrupt cover did not fall back: %s %v", got, err)
					}
				} else {
					w := httptest.NewRecorder()
					m.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", "/artwork/"+key, nil))
					if w.Code != http.StatusNotFound {
						t.Errorf("corrupt handler status: %d", w.Code)
					}
				}
			}()
			select {
			case <-done:
			case <-time.After(300 * time.Millisecond):
				// Stop a regressing recursion before test cleanup releases its file gate.
				m.cancel()
				<-done
				t.Fatal("failed validation retried indefinitely while deletion was denied")
			}
			if got := probeDecodes.Load(); got != 1 {
				t.Fatalf("failed validation decoded %d times; want once", got)
			}
			if stats := m.Stats(); stats.CachedCovers != 1 || stats.UsedBytes != int64(len(data)) {
				t.Fatalf("failed deletion lost accounting: %+v", stats)
			}
		})
	}
}

func TestCoverLockedCorruptionRefetchFallsBackWindows(t *testing.T) {
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "lazy", true: "warm"}[warm], func(t *testing.T) {
			data := picture(t)
			var downloads atomic.Int32
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				downloads.Add(1)
				return response(r, http.StatusOK, data), nil
			})}
			m := manager(t, Options{HTTPClient: client})
			local, err := m.Cover(context.Background(), testCover)
			if err != nil || local == testCover {
				t.Fatalf("initial admission: %s %v", local, err)
			}
			if !warm {
				if err := m.Close(); err != nil {
					t.Fatal(err)
				}
				m = manager(t, Options{Root: m.root, HTTPClient: client})
			}
			key := keyFor("cover", testCover)
			// Corrupt the actual file after admission, retaining its size and the
			// old manifest digest (and, for warm, the old verified MIME record).
			corrupt := append([]byte(nil), data...)
			corrupt[len(corrupt)/2] ^= 0xff
			if err := os.WriteFile(m.filename(key), corrupt, 0600); err != nil {
				t.Fatal(err)
			}
			locked, err := os.Open(m.filename(key))
			if err != nil {
				t.Fatal(err)
			}
			defer locked.Close()
			if err := os.Remove(m.filename(key)); err == nil {
				t.Fatal("file did not deny deletion")
			}
			got, err := m.Cover(context.Background(), testCover)
			if err != nil || got != testCover {
				t.Errorf("unreplaceable corrupt cover advertised local URL: %s %v", got, err)
			}
			if downloads.Load() != 2 {
				t.Fatalf("expected successful refetch, downloads=%d", downloads.Load())
			}
			stored, err := os.ReadFile(m.filename(key))
			if err != nil || !bytes.Equal(stored, corrupt) {
				t.Fatalf("replacement gate did not retain corrupt bytes: %v", err)
			}
			m.mu.Lock()
			_, trusted := m.verified[key]
			m.mu.Unlock()
			if trusted {
				t.Error("failed cleanup retained obsolete trust marker")
			}
			if stats := m.Stats(); stats.CachedCovers != 1 || stats.UsedBytes != int64(len(corrupt)) {
				t.Fatalf("failed replacement lost accounting: %+v", stats)
			}
		})
	}
}
