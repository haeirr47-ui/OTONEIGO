package mediacache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

func seedEntry(tb testing.TB, m *Manager, p []byte) *entry {
	tb.Helper()
	s := candidate()
	key := keyFor(s, "")
	hash := sha256.Sum256(p)
	e := &entry{Key: key, SongID: s.SongID, Provider: s.Provider, Quality: s.ActualQuality, Format: s.ActualFormat, Bitrate: s.ActualBitrate, Duration: int64(len(p)) * 8000 / s.ActualBitrate, Bytes: int64(len(p)), SHA256: hex.EncodeToString(hash[:]), LastAccess: 1}
	if err := os.WriteFile(m.blob(key), p, 0600); err != nil {
		tb.Fatal(err)
	}
	m.entries[key] = e
	if err := m.save(); err != nil {
		tb.Fatal(err)
	}
	return e
}

func TestLookupBatchesRecencyAndCloseFlushes(t *testing.T) {
	root := t.TempDir()
	m := newTest(t, root, nil)
	en := seedEntry(t, m, fixture())
	// First use durably upgrades the legacy record; measure recency after that.
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("initial miss")
	}
	en.LastAccess = 1
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "manifest.json"))
	for i := 0; i < 20; i++ {
		if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
			t.Fatal("cache miss")
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "manifest.json"))
	if !bytes.Equal(before, after) {
		t.Error("20 immediate cache hits rewrote manifest instead of batching recency")
	}
	if en.LastAccess <= 1 {
		t.Fatal("recency not updated in memory")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(filepath.Join(root, "manifest.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("Close rewrote immutable metadata for recency")
	}
	if data, err := os.ReadFile(filepath.Join(root, "recency.json")); err != nil || !bytes.Contains(data, []byte(en.Key)) {
		t.Fatal("Close did not flush latest recency", err)
	}
	again := newTest(t, root, nil)
	if again.entries[en.Key].LastAccess != en.LastAccess {
		t.Fatal("Close recency was not restored")
	}
}

func BenchmarkWarmLookupManifest(b *testing.B) {
	m, err := New(Options{Root: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	seedEntry(b, m, fixture())
	for i := 0; i < 999; i++ {
		e := *m.entries[keyFor(candidate(), "")]
		s := candidate()
		s.SongID = strconv.Itoa(1000 + i)
		e.Key = keyFor(s, "")
		e.SongID = s.SongID
		m.entries[e.Key] = &e
	}
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		b.Fatal("miss")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
			b.Fatal("miss")
		}
	}
}

func BenchmarkColdLookupConcurrentStats(b *testing.B) {
	m, err := New(Options{Root: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	e := seedEntry(b, m, bytes.Repeat(fixture(), 1280))
	b.ResetTimer()
	var blocked time.Duration
	for i := 0; i < b.N; i++ {
		m.mu.Lock()
		e.verified = time.Time{}
		m.mu.Unlock()
		done := make(chan struct{})
		go func() { m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); close(done) }()
		time.Sleep(time.Millisecond)
		start := time.Now()
		m.Stats()
		blocked += time.Since(start)
		<-done
	}
	b.ReportMetric(float64(blocked.Nanoseconds())/float64(b.N), "stats-ns/op")
}

// gateContext pauses an actual cold verification at its first hash chunk. The
// normal Context contract still holds; only the timing of Err is controlled.
type gateContext struct {
	context.Context
	calls   atomic.Int32
	entered chan struct{}
	resume  chan struct{}
}

func (c *gateContext) Err() error {
	if c.calls.Add(1) == 3 {
		close(c.entered)
		<-c.resume
	}
	return c.Context.Err()
}

func TestColdVerificationAllowsStatsAndClearWithoutResurrection(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	seedEntry(t, m, fixture())
	ctx := &gateContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	result := make(chan bool, 1)
	go func() { _, ok := m.Lookup(ctx, "123", "exhigh", domain.PlaybackEntitlement{}); result <- ok }()
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		t.Fatal("cold verification did not reach hash")
	}
	resumed := false
	defer func() {
		if !resumed {
			close(ctx.resume)
		}
	}()
	done := make(chan struct{})
	go func() { m.Stats(); m.Clear(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("verification holds global mutex")
	}
	close(ctx.resume)
	resumed = true
	if <-result {
		t.Fatal("Clear during verification returned a stale cache hit")
	}
	if m.Stats().CachedTracks != 0 {
		t.Fatal("cleared entry resurrected")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	again := newTest(t, m.opt.Root, nil)
	if _, ok := again.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("cleared entry restored after restart")
	}
}

func TestRecencyFlushInterval(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixMilli())
	m, err := New(Options{Root: t.TempDir(), Now: func() time.Time { return time.UnixMilli(now.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	seedEntry(t, m, fixture())
	// Complete the legacy chunk-index upgrade before observing hot recency.
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("initial miss")
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(m.opt.Root, "manifest.json"))
	now.Add(6000)
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("miss")
	}
	deadline := time.Now().Add(2 * time.Second)
	var after []byte
	for time.Now().Before(deadline) {
		after, _ = os.ReadFile(filepath.Join(m.opt.Root, "recency.json"))
		if len(after) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(after) == 0 {
		t.Fatal("reaper did not flush recency")
	}
	now.Add(1)
	for i := 0; i < 20; i++ {
		m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	}
	// This real tick must not rewrite recency until the bounded interval expires.
	time.Sleep(1100 * time.Millisecond)
	latest, _ := os.ReadFile(filepath.Join(m.opt.Root, "recency.json"))
	if !bytes.Equal(after, latest) {
		t.Fatal("recency exceeded bounded write rate")
	}
	manifest, _ := os.ReadFile(filepath.Join(m.opt.Root, "manifest.json"))
	if !bytes.Equal(before, manifest) {
		t.Fatal("reaper rewrote immutable metadata")
	}
}

type waitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *waitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestColdVerificationCoalescesConcurrentAcquisitions(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	e := seedEntry(t, m, fixture())
	first := &gateContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan bool, 2)
	go func() { _, ok := m.Lookup(first, "123", "exhigh", domain.PlaybackEntitlement{}); done <- ok }()
	<-first.entered
	resumed := false
	defer func() {
		if !resumed {
			close(first.resume)
		}
	}()
	second := &waitingContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() { _, ok := m.Lookup(second, "123", "exhigh", domain.PlaybackEntitlement{}); done <- ok }()
	select {
	case <-second.waiting:
	case <-time.After(time.Second):
		t.Fatal("second acquisition did not join active verification")
	}
	m.mu.Lock()
	pins := m.pins[e.Key]
	m.mu.Unlock()
	if pins != 1 {
		t.Fatalf("concurrent acquisition performed independent verification: %d pins", pins)
	}
	close(first.resume)
	resumed = true
	if !<-done || !<-done {
		t.Fatal("coalesced acquisition missed verified entry")
	}
}

func TestPrepareClearDuringVerificationDoesNotCreateReplacementJob(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	seedEntry(t, m, fixture())
	ctx := &gateContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan domain.ResolvedStream, 1)
	go func() { s, _ := m.Prepare(ctx, candidate()); done <- s }()
	<-ctx.entered
	m.Clear()
	close(ctx.resume)
	if s := <-done; s.CacheToken != "" {
		t.Fatal("cleared acquisition obtained a lease")
	}
	m.mu.Lock()
	jobs := len(m.jobs)
	m.mu.Unlock()
	if jobs != 0 {
		t.Fatal("Clear during verification recreated a cache download")
	}
}

func TestPrepareCorruptBlobClearDuringVerificationDoesNotRecreate(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	p := fixture()
	e := seedEntry(t, m, p)
	p[len(p)-1] ^= 1
	if err := os.WriteFile(m.blob(e.Key), p, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := &gateContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan domain.ResolvedStream, 1)
	go func() { s, _ := m.Prepare(ctx, candidate()); done <- s }()
	<-ctx.entered
	if _, err := m.Clear(); err != nil {
		close(ctx.resume)
		t.Fatal(err)
	}
	close(ctx.resume)
	s := <-done
	m.mu.Lock()
	jobs, leases := len(m.jobs), len(m.leases)
	m.mu.Unlock()
	if s.CacheToken != "" || jobs != 0 || leases != 0 {
		t.Fatalf("corrupt verification recreated state after Clear: token=%t jobs=%d leases=%d", s.CacheToken != "", jobs, leases)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newTest(t, m.opt.Root, nil)
	if _, ok := reopened.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("cleared corrupt entry restored after restart")
	}
}

// Entries published while a cold verification is unlocked belong to the next
// acquisition, even when the original candidate fails its integrity check.
func TestLookupUsesEligibleSnapshotAcrossUnlockedVerification(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	e := seedEntry(t, m, fixture())
	e.SHA256 = strings.Repeat("0", 64)
	ctx := &gateContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan bool, 1)
	go func() { _, ok := m.Lookup(ctx, "123", "exhigh", domain.PlaybackEntitlement{}); done <- ok }()
	<-ctx.entered
	m.mu.Lock()
	// Grow the map while its original candidate is being verified outside mu.
	// Every newly published candidate is valid but outside the acquisition snapshot.
	for i := 0; i < 64; i++ {
		s := candidate()
		s.ActualBitrate -= int64(i + 1)
		replacement := *e
		replacement.Key = keyFor(s, "")
		replacement.Bitrate = s.ActualBitrate
		hash := sha256.Sum256(fixture())
		replacement.SHA256 = hex.EncodeToString(hash[:])
		replacement.verifying = nil
		if err := os.WriteFile(m.blob(replacement.Key), fixture(), 0600); err != nil {
			t.Fatal(err)
		}
		m.entries[replacement.Key] = &replacement
	}
	m.mu.Unlock()
	close(ctx.resume)
	if <-done {
		t.Fatal("lookup consumed an entry published after its eligible snapshot")
	}
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("next acquisition did not see newly published candidate")
	}
}

func BenchmarkCold100MiBLookupFirstByte(b *testing.B) {
	b.Run("legacy", func(b *testing.B) { benchmarkColdFirstByte(b, false) })
	b.Run("indexed", func(b *testing.B) { benchmarkColdFirstByte(b, true) })
}
func benchmarkColdFirstByte(b *testing.B, indexed bool) {
	m, err := New(Options{Root: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	payload := bytes.Repeat(fixture(), 1005) // 100.06 MiB, valid MP3 frame boundaries.
	e := seedEntry(b, m, payload)
	var index chunkDigests
	index.Write(payload)
	hashes := index.finish()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.mu.Lock()
		e.verified = time.Time{}
		e.Chunks = nil
		if indexed {
			e.Chunks = hashes
		}
		m.mu.Unlock()
		hit, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
		if !ok {
			b.Fatal("cold miss")
		}
		local, err := m.Prepare(context.Background(), hit)
		if err != nil {
			b.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, local.URL, nil)
		req.Header.Set("Range", "bytes=0-0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		var first [1]byte
		_, err = io.ReadFull(resp.Body, first[:])
		resp.Body.Close()
		m.Release(local.CacheToken)
		if err != nil || resp.StatusCode != http.StatusPartialContent || first[0] != payload[0] {
			b.Fatal("first byte", err)
		}
	}
}
