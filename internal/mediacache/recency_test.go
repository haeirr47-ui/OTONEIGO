package mediacache

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestCompletedDownloadManifestFailureRecovery(t *testing.T) {
	for _, recovery := range []string{"Close", "reaper"} {
		t.Run(recovery, func(t *testing.T) {
			root := t.TempDir()
			payload := fixture()
			var now atomic.Int64
			now.Store(time.Now().UnixMilli())
			m, err := New(Options{Root: root, Now: func() time.Time { return time.UnixMilli(now.Load()) }, HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { m.Close() })
			blocked := filepath.Join(root, "manifest.tmp")
			if err := os.Mkdir(blocked, 0700); err != nil {
				t.Fatal(err)
			}
			s, err := m.Prepare(context.Background(), candidate())
			if err != nil {
				t.Fatal(err)
			}
			response := get(t, s.URL, "")
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			waitDone(t, m)
			m.Release(s.CacheToken)
			if m.Stats().CachedTracks != 1 {
				t.Fatal("download did not commit to memory")
			}
			if recovery == "reaper" {
				// A failed retry must advance its attempt clock, then remain bounded
				// even after the filesystem recovers and lookups mark recency dirty.
				now.Add(6000)
				deadline := time.Now().Add(2500 * time.Millisecond)
				for {
					m.mu.Lock()
					attempted := m.lastSaveAttempt.UnixMilli() == now.Load() && m.structuralDirty
					m.mu.Unlock()
					if attempted {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("reaper did not retry failed structural save")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			if err := os.Remove(blocked); err != nil {
				t.Fatal(err)
			}
			if recovery == "reaper" {
				now.Add(1)
				if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
					t.Fatal("memory lookup missed")
				}
				time.Sleep(1100 * time.Millisecond)
				m.mu.Lock()
				dirty := m.structuralDirty
				m.mu.Unlock()
				if !dirty {
					t.Fatal("structural retry exceeded five-second write bound")
				}
				now.Add(5000)
				deadline := time.Now().Add(2500 * time.Millisecond)
				for {
					m.mu.Lock()
					dirty = m.structuralDirty
					m.mu.Unlock()
					if !dirty {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("reaper did not recover structural commit")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			again := newTest(t, root, nil)
			if _, ok := again.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
				t.Fatal("failed structural commit was not recovered; completed cache lost on restart")
			}
		})
	}
}

// Synthetic immutable indexes represent 20 GiB without writing audio payloads.
// Only the real entry is looked up, through the normal verification path.
func seed20GiBIndex(t testing.TB, m *Manager) *entry {
	t.Helper()
	e := seedEntry(t, m, fixture())
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("initial lookup missed")
	}
	for i := 0; i < 1024; i++ {
		s := candidate()
		s.SongID = strconv.Itoa(1000 + i)
		v := *e
		v.Key, v.SongID = keyFor(s, ""), s.SongID
		v.Bytes = 20 * 1024 * 1024
		v.Chunks = make([]string, 20)
		for j := range v.Chunks {
			v.Chunks[j] = strings.Repeat("a", 64)
		}
		m.entries[v.Key] = &v
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	return e
}

func Test20GiBIndexRecencyCloseDoesNotRewriteChunks(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixMilli())
	m, err := New(Options{Root: t.TempDir(), Now: func() time.Time { return time.UnixMilli(now.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	seed20GiBIndex(t, m)
	path := filepath.Join(m.opt.Root, "manifest.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	now.Add(6000)
	for i := 0; i < 20; i++ {
		if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
			t.Fatal("warm lookup missed")
		}
	}
	deadline := time.Now().Add(2500 * time.Millisecond)
	for {
		if _, err := os.Stat(filepath.Join(m.opt.Root, "recency.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reaper did not persist recency")
		}
		time.Sleep(10 * time.Millisecond)
	}
	now.Add(1)
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("final lookup missed")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	latest, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !st.ModTime().Equal(latest.ModTime()) {
		t.Fatalf("recency rewrote immutable index: %d bytes", len(after))
	}
	recency, err := os.ReadFile(filepath.Join(m.opt.Root, "recency.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recency) > 1024 {
		t.Fatalf("single touched entry wrote %d bytes", len(recency))
	}
	t.Logf("20 GiB synthetic index: manifest=%d bytes; recency=%d bytes; manifest mtime unchanged", len(before), len(recency))
}

func TestRecencyOverlayCannotResurrectOrTouchReplacement(t *testing.T) {
	for _, action := range []string{"replacement", "clear"} {
		t.Run(action, func(t *testing.T) {
			m := newTest(t, t.TempDir(), nil)
			e := seedEntry(t, m, fixture())
			if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
				t.Fatal("miss")
			}
			m.mu.Lock()
			if err := m.saveRecency(); err != nil {
				t.Fatal(err)
			}
			m.mu.Unlock()
			stale, err := os.ReadFile(filepath.Join(m.opt.Root, "recency.json"))
			if err != nil {
				t.Fatal(err)
			}
			if action == "clear" {
				if _, err := m.Clear(); err != nil {
					t.Fatal(err)
				}
			} else {
				// Same key and audio, with a repeated/backwards replacement clock.
				replacement := *e
				replacement.Generation, replacement.LastAccess = "", 1
				m.entries[e.Key] = &replacement
				if err := m.save(); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate a crash leaving an older complete sidecar on disk.
			if err := os.WriteFile(filepath.Join(m.opt.Root, "recency.json"), stale, 0600); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			again := newTest(t, m.opt.Root, nil)
			if action == "clear" {
				if len(again.entries) != 0 {
					t.Fatal("sidecar resurrected cleared audio")
				}
			} else if again.entries[e.Key].LastAccess != 1 {
				t.Fatal("stale recency applied to replacement")
			}
		})
	}
}

func TestRecencyCorruptOrOversizedSidecarPreservesAudio(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{"broken":`), bytes.Repeat([]byte(" "), maxRecency+1)} {
		m := newTest(t, t.TempDir(), nil)
		e := seedEntry(t, m, fixture())
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(m.opt.Root, "recency.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		again := newTest(t, m.opt.Root, nil)
		if again.entries[e.Key] == nil {
			t.Fatal("bad optional recency removed valid audio")
		}
		if _, ok := again.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
			t.Fatal("bad optional recency made audio unusable")
		}
	}
}

func TestRecencySnapshotExcludesPendingAndInvalid(t *testing.T) {
	for _, state := range []string{"pending", "invalid"} {
		m := newTest(t, t.TempDir(), nil)
		e := seedEntry(t, m, fixture())
		m.mu.Lock()
		m.touch(e)
		if state == "pending" {
			m.pending[e.Key] = true
		} else {
			m.invalid[e.Key] = true
		}
		m.mu.Unlock()
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(m.opt.Root, "recency.json"))
		if err != nil {
			t.Fatal(err)
		}
		var records map[string]recencyRecord
		if err := json.Unmarshal(b, &records); err != nil {
			t.Fatal(err)
		}
		if len(records) != 0 {
			t.Fatal("excluded audio leaked into recency snapshot")
		}
	}
}

func TestRecencyFailedReaperFlushRetriesAtBoundedInterval(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixMilli())
	m, err := New(Options{Root: t.TempDir(), Now: func() time.Time { return time.UnixMilli(now.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	seedEntry(t, m, fixture())
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("miss")
	}
	blocked := filepath.Join(m.opt.Root, "recency.tmp")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	now.Add(6000)
	waitFor := func(predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(2500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if predicate() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("reaper did not reach expected persistence state")
	}
	waitFor(func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.lastSaveAttempt.UnixMilli() == now.Load() && m.recencyDirty
	})
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	now.Add(1)
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(m.opt.Root, "recency.json")); !os.IsNotExist(err) {
		t.Fatal("failed flush retried before five seconds", err)
	}
	now.Add(5000)
	waitFor(func() bool { _, err := os.Stat(filepath.Join(m.opt.Root, "recency.json")); return err == nil })
}

func Benchmark20GiBIndexLookupClose(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m, err := New(Options{Root: b.TempDir()})
		if err != nil {
			b.Fatal(err)
		}
		seed20GiBIndex(b, m)
		st, err := os.Stat(filepath.Join(m.opt.Root, "manifest.json"))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(st.Size()), "index-bytes")
		b.StartTimer()
		if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
			b.Fatal("miss")
		}
		if err := m.Close(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		r, err := os.Stat(filepath.Join(m.opt.Root, "recency.json"))
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(r.Size()), "recency-bytes")
	}
}
