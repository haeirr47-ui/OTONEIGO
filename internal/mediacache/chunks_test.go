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
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

func indexedEntry(t *testing.T, m *Manager) (*entry, []byte) {
	t.Helper()
	p := bytes.Repeat(fixture(), 41) // >4 chunks, valid complete frame boundary.
	e := seedEntry(t, m, p)
	var hashes chunkDigests
	// Deliberately use non-chunk transport boundaries.
	for off := 0; off < len(p); {
		end := off + 7777
		if end > len(p) {
			end = len(p)
		}
		hashes.Write(p[off:end])
		off = end
	}
	e.Chunks = hashes.finish()
	if !validChunks(e) {
		t.Fatal("invalid seeded chunks")
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	return e, p
}
func mutateByte(t *testing.T, m *Manager, e *entry, off int64) {
	t.Helper()
	f, err := os.OpenFile(m.blob(e.Key), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteAt([]byte{0x7f}, off); err != nil {
		t.Fatal(err)
	}
}
func TestIndexedCacheRejectsCorruptInitialRangeBeforeHeaders(t *testing.T) {
	for _, off := range []int64{0, integrityChunk + 123} {
		t.Run(string(rune('a'+off/integrityChunk)), func(t *testing.T) {
			m := newTest(t, t.TempDir(), nil)
			e, p := indexedEntry(t, m)
			hit, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
			if !ok {
				t.Fatal("miss")
			}
			local, err := m.Prepare(context.Background(), hit)
			if err != nil {
				t.Fatal(err)
			}
			mutateByte(t, m, e, off)
			// Keep a stable mtime: indexed chunks may never use the legacy stamp bypass.
			_ = os.Chtimes(m.blob(e.Key), time.Now(), e.verified)
			req, _ := http.NewRequest(http.MethodGet, local.URL, nil)
			req.Header.Set("Range", "bytes="+strconv.FormatInt(off, 10)+"-"+strconv.FormatInt(off+31, 10))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 410 || bytes.Equal(b, p[off:off+32]) {
				t.Fatalf("corrupt initial chunk status=%d bytes=%d", resp.StatusCode, len(b))
			}
			resp = get(t, local.URL, "bytes=0-31")
			resp.Body.Close()
			if resp.StatusCode != 410 {
				t.Fatal("corrupt lease reused", resp.StatusCode)
			}
			if _, ok = m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
				t.Fatal("corrupt cache discoverable")
			}
		})
	}
}
func TestIndexedCacheAbortsBeforeLaterCorruptChunk(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	e, p := indexedEntry(t, m)
	hit, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	if !ok {
		t.Fatal("miss")
	}
	local, err := m.Prepare(context.Background(), hit)
	if err != nil {
		t.Fatal(err)
	}
	mutateByte(t, m, e, 2*integrityChunk+7)
	resp := get(t, local.URL, "")
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err == nil || len(b) > int(2*integrityChunk) || !bytes.Equal(b, p[:len(b)]) {
		t.Fatalf("damaged chunk escaped: bytes=%d err=%v", len(b), err)
	}
	resp = get(t, local.URL, "bytes=0-31")
	resp.Body.Close()
	if resp.StatusCode != 410 {
		t.Fatal("later corruption did not invalidate lease")
	}
	manifest, _ := os.ReadFile(filepath.Join(m.opt.Root, "manifest.json"))
	if bytes.Contains(manifest, []byte(e.Key)) {
		t.Fatal("corrupt entry remained persisted")
	}
}
func TestChunkReaderReturnsTheVerifiedBuffer(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	e, p := indexedEntry(t, m)
	f, err := os.Open(m.blob(e.Key))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := newChunkReader(context.Background(), f, e, nil)
	if err = r.load(0); err != nil {
		t.Fatal(err)
	}
	mutateByte(t, m, e, 17)
	got := make([]byte, 32)
	if _, err = r.ReadAt(got, 0); err != nil || !bytes.Equal(got, p[:32]) {
		t.Fatal("verified buffer was reread from disk", err)
	}
}
func TestLegacyUpgradePersistsOnlyAfterCompleteIntegrityCheck(t *testing.T) {
	root := t.TempDir()
	m := newTest(t, root, nil)
	e := seedEntry(t, m, bytes.Repeat(fixture(), 41))
	if len(e.Chunks) != 0 {
		t.Fatal("seed not legacy")
	}
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok || !validChunks(e) {
		t.Fatal("legacy did not upgrade")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	again := newTest(t, root, nil)
	if !validChunks(again.entries[e.Key]) {
		t.Fatal("upgraded chunks lost on restart")
	}
	if _, ok := again.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("upgraded cache miss")
	}
}
func TestMalformedChunkIndexExcludedOnLoad(t *testing.T) {
	for _, kind := range []string{"count", "hex", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			m := newTest(t, root, nil)
			e, _ := indexedEntry(t, m)
			switch kind {
			case "count":
				e.Chunks = e.Chunks[:1]
			case "hex":
				e.Chunks[0] = strings.Repeat("A", 64)
			case "oversize":
				e.Chunks = make([]string, 513)
			}
			data, _ := json.Marshal(map[string]*entry{e.Key: e})
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			again := newTest(t, root, nil)
			if again.Stats().CachedTracks != 0 {
				t.Fatal("malformed index accepted")
			}
		})
	}
}
func TestIndexedVerificationClearDoesNotPublishStaleHit(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	indexedEntry(t, m)
	ctx := &gateContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan bool, 1)
	go func() { _, ok := m.Lookup(ctx, "123", "exhigh", domain.PlaybackEntitlement{}); done <- ok }()
	<-ctx.entered
	if _, err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	close(ctx.resume)
	if <-done {
		t.Fatal("indexed verification resurrected cleared entry")
	}
	if m.Stats().CachedTracks != 0 {
		t.Fatal("cleared indexed record retained")
	}
}
func TestIndexedReaderCanceledAndClosedManager(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	e, _ := indexedEntry(t, m)
	f, err := os.Open(m.blob(e.Key))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := newChunkReader(ctx, f, e, func() { t.Error("cancellation marked corruption") })
	if _, err = r.Read(make([]byte, 32)); err != context.Canceled {
		t.Fatal("reader ignored cancellation", err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("closed manager returned indexed entry")
	}
}

func TestIndexedLookupDefersMiddleBytesUntilDemanded(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	e, _ := indexedEntry(t, m)
	mutateByte(t, m, e, 2*integrityChunk+7)
	// A cold indexed lookup checks structure and its touched chunks, not unrelated
	// audio. The requested middle chunk must still be verified before its bytes.
	e.verified = time.Time{}
	hit, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	if !ok {
		t.Fatal("indexed lookup scanned unrelated middle audio")
	}
	local, err := m.Prepare(context.Background(), hit)
	if err != nil {
		t.Fatal(err)
	}
	resp := get(t, local.URL, "bytes=2097152-2097183")
	resp.Body.Close()
	if resp.StatusCode != 410 {
		t.Fatal("deferred corrupt chunk was emitted", resp.StatusCode)
	}
}
func TestIndexedRangeBoundariesAndHead(t *testing.T) {
	m := newTest(t, t.TempDir(), nil)
	_, p := indexedEntry(t, m)
	hit, _ := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	local, err := m.Prepare(context.Background(), hit)
	if err != nil {
		t.Fatal(err)
	}
	for _, rng := range []string{"bytes=1048560-1048591", "bytes=-32", "bytes=0-0"} {
		resp := get(t, local.URL, rng)
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		start, end, _ := parseRange(rng, int64(len(p)))
		if err != nil || resp.StatusCode != 206 || !bytes.Equal(b, p[start:end+1]) {
			t.Fatalf("range %s mismatch: %d %v", rng, resp.StatusCode, err)
		}
	}
	req, _ := http.NewRequest(http.MethodHead, local.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || len(b) != 0 || resp.StatusCode != 200 || resp.ContentLength != int64(len(p)) {
		t.Fatal("indexed HEAD contract", resp.StatusCode, err)
	}
}
