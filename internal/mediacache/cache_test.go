package mediacache

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture() []byte {
	p := make([]byte, 1044*100)
	for i := 0; i < len(p); i += 1044 {
		binary.BigEndian.PutUint32(p[i:], 0xfffbe000)
	}
	return p
}
func candidate() domain.ResolvedStream {
	return domain.ResolvedStream{SongID: "123", Provider: "netease-direct", URL: "https://m7.music.126.net/full.mp3?secret=signed", Status: "full", RequestedQuality: "exhigh", ActualQuality: "exhigh", ActualFormat: "mp3", ActualBitrate: 320000, DurationMs: 2606}
}
func newTest(t *testing.T, root string, rt roundTrip) *Manager {
	t.Helper()
	m, e := New(Options{Root: root, HTTPClient: &http.Client{Transport: rt}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func get(t *testing.T, raw, rng string) *http.Response {
	t.Helper()
	r, _ := http.NewRequest("GET", raw, nil)
	r.Header.Set("Range", rng)
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	return resp
}
func waitDone(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.Stats().ActiveDownloads == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("download not finished")
}
func TestProgressiveOneFetchRangeRestartIntegrityAndSafeManifest(t *testing.T) {
	root := t.TempDir()
	payload := fixture()
	unblock := make(chan struct{})
	var calls atomic.Int32
	m := newTest(t, root, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Real-IP") != "" {
			t.Error("auth leaked")
		}
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			_, _ = pw.Write(payload[:32768])
			select {
			case <-unblock:
			case <-r.Context().Done():
				return
			}
			_, _ = pw.Write(payload[32768:])
		}()
		return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: pr, Header: make(http.Header)}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	s, e := m.Prepare(ctx, candidate())
	if e != nil || s.CacheToken == "" {
		t.Fatal(s, e)
	}
	cancel()
	resp := get(t, s.URL, "")
	first := make([]byte, 128)
	if _, e = io.ReadFull(resp.Body, first); e != nil || !bytes.Equal(first, payload[:128]) {
		t.Fatal("first bytes before EOF", e)
	}
	rangeDone := make(chan []byte, 1)
	go func() {
		r, _ := http.NewRequest("GET", s.URL, nil)
		r.Header.Set("Range", "bytes=100000-100099")
		rs, e := http.DefaultClient.Do(r)
		if e != nil {
			rangeDone <- nil
			return
		}
		defer rs.Body.Close()
		b, _ := io.ReadAll(rs.Body)
		rangeDone <- b
	}()
	close(unblock)
	rest, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || len(rest)+len(first) != len(payload) {
		t.Fatal(e, len(rest))
	}
	if got := <-rangeDone; !bytes.Equal(got, payload[100000:100100]) {
		t.Fatal("range bytes", len(got))
	}
	waitDone(t, m)
	if calls.Load() != 1 || m.Stats().CachedTracks != 1 {
		t.Fatal(calls.Load(), m.Stats())
	}
	if en := m.entries[keyFor(candidate(), "")]; en == nil || !validChunks(en) {
		t.Fatal("validated download commit did not publish chunk integrity index")
	}
	manifest, e := os.ReadFile(filepath.Join(root, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"https:", "secret", "signed", s.CacheToken, s.URL} {
		if bytes.Contains(manifest, []byte(secret)) {
			t.Fatal("persisted credential", secret)
		}
	}
	hit, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	if !ok || !hit.CacheHit {
		t.Fatal("missing hit")
	}
	if _, ok = m.Lookup(context.Background(), "123", "higher", domain.PlaybackEntitlement{}); ok {
		t.Fatal("quality upgraded")
	}
	m.Release(s.CacheToken)
	m.Close()
	offline := newTest(t, root, func(*http.Request) (*http.Response, error) { t.Error("offline fetched network"); return nil, errCache })
	hit, ok = offline.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	if !ok {
		t.Fatal("restart missing")
	}
	local, e := offline.Prepare(context.Background(), hit)
	if e != nil {
		t.Fatal(e)
	}
	rs := get(t, local.URL, "bytes=-100")
	b, _ := io.ReadAll(rs.Body)
	rs.Body.Close()
	if rs.StatusCode != 206 || !bytes.Equal(b, payload[len(payload)-100:]) {
		t.Fatal("offline range")
	}
	offline.Release(local.CacheToken)
	if e = os.WriteFile(offline.blob(hit.CacheEntryID), payload[:20], 0600); e != nil {
		t.Fatal(e)
	}
	if _, ok = offline.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("accepted corruption")
	}
}
func TestClearRetainsActiveAndReleaseDeletes(t *testing.T) {
	p := fixture()
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	c, e := m.Clear()
	if e != nil || c.RetainedActiveTracks != 1 || m.Stats().CachedTracks != 1 {
		t.Fatal(c, e, m.Stats())
	}
	m.Release(s.CacheToken)
	if m.Stats().CachedTracks != 0 {
		t.Fatal("clear not completed")
	}
}
func TestRejectInvalidDownloads(t *testing.T) {
	for _, tc := range []struct {
		name     string
		data     []byte
		size     int64
		duration int64
	}{{"html", []byte("<html>not audio</html>"), 22, 2606}, {"truncated", fixture()[:100], int64(len(fixture())), 2606}, {"duration", fixture(), int64(len(fixture())), 60000}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: tc.size, Body: io.NopCloser(bytes.NewReader(tc.data))}, nil
			})
			s := candidate()
			s.DurationMs = tc.duration
			s, _ = m.Prepare(context.Background(), s)
			r := get(t, s.URL, "")
			_, _ = io.Copy(io.Discard, r.Body)
			r.Body.Close()
			waitDone(t, m)
			if m.Stats().CachedTracks != 0 {
				t.Fatal("invalid cached")
			}
		})
	}
}
func TestRoutingUnsafeURLsAndCancellation(t *testing.T) {
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) { t.Fatal("unexpected upstream"); return nil, errCache })
	for _, raw := range []string{"http://m7.music.126.net/a", "https://evil.test/a", "https://m7.music.126.net.evil/a", "https://u:p@m7.music.126.net/a", "https://m7.music.126.net:443/a"} {
		s := candidate()
		s.URL = raw
		if _, e := m.Prepare(context.Background(), s); e == nil {
			t.Fatal("unsafe accepted", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := m.Prepare(ctx, candidate()); e == nil {
		t.Fatal("cancelled prepared")
	}
	s, _ := m.Prepare(context.Background(), candidate())
	for _, tail := range []string{"wrong", "../manifest.json", s.CacheToken + "?x=1"} {
		r := get(t, "http://"+m.host+"/media/"+tail, "")
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatal(tail, r.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", s.URL, nil)
	req.Host = "evil.test"
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatal("Host accepted")
	}
	m.Release(s.CacheToken)
	m.Release(s.CacheToken)
}
func TestAccountScopeAndPremiumPolicy(t *testing.T) {
	s := candidate()
	own := scope(domain.PlaybackEntitlement{LoggedIn: true, AccountID: "private-account"})
	if strings.Contains(own, "private") || len(own) != 64 {
		t.Fatal(own)
	}
	if allowed(s, own, domain.PlaybackEntitlement{}) || allowed(s, own, domain.PlaybackEntitlement{LoggedIn: true, AccountID: "other"}) {
		t.Fatal("account crossed")
	}
	s.ActualQuality = "lossless"
	s.ActualBitrate = 900000
	for _, ent := range []domain.PlaybackEntitlement{{}, {Member: true}, {LoggedIn: true, Member: true}, {LoggedIn: true, AccountID: "private-account"}} {
		if allowed(s, own, ent) {
			t.Fatal("premium allowed", ent)
		}
	}
	if !allowed(s, own, domain.PlaybackEntitlement{LoggedIn: true, Member: true, AccountID: "private-account"}) {
		t.Fatal("verified member denied")
	}
}
func TestQuotaReservationsLRUAndSettingsDuringPlay(t *testing.T) {
	p := fixture()
	settings := domain.AudioCacheSettings{Enabled: true, MaxBytes: int64(len(p))*2 + 1}
	now := time.Unix(1, 0)
	m, e := New(Options{Root: t.TempDir(), Settings: func() domain.AudioCacheSettings { return settings }, Now: func() time.Time { return now }, HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})}})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	play := func(id string) domain.ResolvedStream {
		s := candidate()
		s.SongID = id
		s, e = m.Prepare(context.Background(), s)
		if e != nil {
			t.Fatal(e)
		}
		r := get(t, s.URL, "")
		_, _ = io.Copy(io.Discard, r.Body)
		r.Body.Close()
		waitDone(t, m)
		now = now.Add(time.Second)
		return s
	}
	first := play("1")
	m.Release(first.CacheToken)
	second := play("2")
	m.Release(second.CacheToken)
	third := play("3")
	if _, ok := m.Lookup(context.Background(), "1", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("LRU retained")
	}
	if m.Stats().UsedBytes > settings.MaxBytes {
		t.Fatal("quota exceeded")
	}
	settings.MaxBytes = 1
	settings.Enabled = false
	m.ApplySettings()
	r := get(t, third.URL, "bytes=0-31")
	b, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || len(b) != 32 {
		t.Fatal("active lease broken", e)
	}
	unsafe := candidate()
	unsafe.URL = "http://evil.test/file"
	if _, e = m.Prepare(context.Background(), unsafe); e == nil {
		t.Fatal("disabled cache bypassed URL policy")
	}
	m.Release(third.CacheToken)
	if m.Stats().CachedTracks != 0 {
		t.Fatal("quota failed after release")
	}
}
func TestLazyReleaseHeadAndRangeErrors(t *testing.T) {
	var calls atomic.Int32
	p := fixture()
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	if calls.Load() != 0 {
		t.Fatal("eager fetch")
	}
	m.Release(s.CacheToken)
	s, _ = m.Prepare(context.Background(), candidate())
	req, _ := http.NewRequest("HEAD", s.URL, nil)
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.ContentLength != int64(len(p)) || r.StatusCode != 200 {
		t.Fatal(r.StatusCode, r.ContentLength)
	}
	waitDone(t, m)
	for _, rng := range []string{"bytes=999999999-", "bytes=0-1,3-4", "bytes=a-b", "bytes=-0"} {
		r = get(t, s.URL, rng)
		r.Body.Close()
		if r.StatusCode != 416 {
			t.Fatal(rng, r.StatusCode)
		}
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
func TestReportFailureInvalidatesButProtectsOtherLease(t *testing.T) {
	p := fixture()
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	hit, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{})
	if !ok {
		t.Fatal("missing valid hash cache")
	}
	other, e := m.Prepare(context.Background(), hit)
	if e != nil {
		t.Fatal(e)
	}
	m.ReportFailure(s.CacheToken)
	m.ReportFailure(s.CacheToken)
	if _, ok = m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("reported failed blob reused")
	}
	if _, e = m.Prepare(context.Background(), hit); e == nil {
		t.Fatal("stale cache selection accepted")
	}
	direct, e := m.Prepare(context.Background(), candidate())
	if e != nil || direct.CacheToken != "" || direct.URL != candidate().URL {
		t.Fatal("new selection reused invalid job", direct, e)
	}
	if _, e = os.Stat(m.blob(hit.CacheEntryID)); e != nil {
		t.Fatal("other lease lost physical file", e)
	}
	r = get(t, other.URL, "bytes=0-31")
	b, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || !bytes.Equal(b, p[:32]) {
		t.Fatal("other lease cannot finish", e)
	}
	manifest, e := os.ReadFile(filepath.Join(m.opt.Root, "manifest.json"))
	if e != nil || bytes.Contains(manifest, []byte(hit.CacheEntryID)) {
		t.Fatal("failure persisted across restart", e)
	}
	m.Release(other.CacheToken)
	if _, e = os.Stat(m.blob(hit.CacheEntryID)); !os.IsNotExist(e) {
		t.Fatal("failed file retained after last release", e)
	}
	next, e := m.Prepare(context.Background(), candidate())
	if e != nil || next.CacheToken == "" {
		t.Fatal("failed key cannot retry after eviction", e)
	}
	m.Release(next.CacheToken)
}
func TestRootLifetimeLockProtectsActivePartialAndReopens(t *testing.T) {
	root := t.TempDir()
	p := fixture()
	release := make(chan struct{})
	sent := make(chan struct{})
	m := newTest(t, root, func(r *http.Request) (*http.Response, error) {
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			_, _ = pw.Write(p[:32768])
			close(sent)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = pw.Write(p[32768:])
		}()
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: pr, Header: make(http.Header)}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	first := make([]byte, 128)
	if _, e := io.ReadFull(r.Body, first); e != nil {
		t.Fatal(e)
	}
	<-sent
	part := filepath.Join(root, s.CacheEntryID+".part")
	before, e := os.Stat(part)
	if e != nil {
		t.Fatal(e)
	}
	second, e := New(Options{Root: root})
	if second != nil || e != ErrInUse {
		if second != nil {
			second.Close()
		}
		t.Fatal("second owner accepted", e)
	}
	after, e := os.Stat(part)
	if e != nil || after.Size() != before.Size() {
		t.Fatal("second manager touched primary partial", e)
	}
	close(release)
	rest, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || len(rest)+len(first) != len(p) {
		t.Fatal("primary interrupted", e)
	}
	waitDone(t, m)
	m.Release(s.CacheToken)
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := New(Options{Root: root})
	if e != nil {
		t.Fatal("lock retained after Close", e)
	}
	defer reopened.Close()
	if _, ok := reopened.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); !ok {
		t.Fatal("cache lost on reopen")
	}
}
func TestVBRNonRoundGradeRestartOfflineHit(t *testing.T) {
	p := make([]byte, 417*100)
	for off := 0; off < len(p); off += 417 {
		binary.BigEndian.PutUint32(p[off:], 0xfffb9000)
	}
	copy(p[36:], "Xing")
	binary.BigEndian.PutUint32(p[40:], 3)
	binary.BigEndian.PutUint32(p[44:], 100)
	binary.BigEndian.PutUint32(p[48:], uint32(len(p)))
	root := t.TempDir()
	m := newTest(t, root, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s := candidate()
	s.ActualQuality = "standard"
	s.RequestedQuality = "standard"
	s.ActualBitrate = 127706
	s.DurationMs = 2612
	s, _ = m.Prepare(context.Background(), s)
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	m.Release(s.CacheToken)
	m.Close()
	offline := newTest(t, root, func(*http.Request) (*http.Response, error) {
		t.Error("offline VBR network fetch")
		return nil, errCache
	})
	hit, ok := offline.Lookup(context.Background(), "123", "standard", domain.PlaybackEntitlement{})
	if !ok || hit.ActualBitrate != 127706 {
		t.Fatal("VBR grade lost", hit)
	}
	if _, ok = offline.Lookup(context.Background(), "123", "higher", domain.PlaybackEntitlement{}); ok {
		t.Fatal("VBR grade upgraded")
	}
	local, e := offline.Prepare(context.Background(), hit)
	if e != nil {
		t.Fatal(e)
	}
	r = get(t, local.URL, "")
	b, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || !bytes.Equal(b, p) {
		t.Fatal("VBR offline playback", e)
	}
}
func TestClearActiveLeaseNotRestoredAfterClose(t *testing.T) {
	root := t.TempDir()
	p := fixture()
	m := newTest(t, root, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	if _, e := m.Clear(); e != nil {
		t.Fatal(e)
	}
	m.Close()
	reopened := newTest(t, root, func(*http.Request) (*http.Response, error) { t.Error("unexpected fetch"); return nil, errCache })
	if _, ok := reopened.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("cleared leased entry restored")
	}
	if _, e := os.Stat(m.blob(s.CacheEntryID)); !os.IsNotExist(e) {
		t.Fatal("cleared orphan retained", e)
	}
}
func TestClearActiveDownloadFinishingNeverRestored(t *testing.T) {
	root := t.TempDir()
	p := fixture()
	finish := make(chan struct{})
	m := newTest(t, root, func(r *http.Request) (*http.Response, error) {
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			_, _ = pw.Write(p[:32768])
			select {
			case <-finish:
			case <-r.Context().Done():
				return
			}
			_, _ = pw.Write(p[32768:])
		}()
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: pr, Header: make(http.Header)}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	first := make([]byte, 32)
	if _, e := io.ReadFull(r.Body, first); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Clear(); e != nil {
		t.Fatal(e)
	}
	close(finish)
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	m.Close()
	reopened := newTest(t, root, func(*http.Request) (*http.Response, error) { t.Error("unexpected fetch"); return nil, errCache })
	if _, ok := reopened.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("download persisted after Clear")
	}
	if _, e := os.Stat(m.blob(s.CacheEntryID)); !os.IsNotExist(e) {
		t.Fatal("cleared download orphan retained", e)
	}
}
func TestUnsupportedContainerAndKnownOversizeRemainDirect(t *testing.T) {
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) { t.Error("unexpected proxy fetch"); return nil, errCache })
	for _, format := range []string{"m4a", "ogg", "aac", "unknown"} {
		s := candidate()
		s.ActualFormat = format
		got, e := m.Prepare(context.Background(), s)
		if e != nil || got.URL != s.URL || got.CacheToken != "" {
			t.Fatal("unsupported format broke direct playback", format, got, e)
		}
	}
	s := candidate()
	s.ContentLengthBytes = maxAudio + 1
	got, e := m.Prepare(context.Background(), s)
	if e != nil || got.URL != s.URL || got.CacheToken != "" {
		t.Fatal("oversize proxied", got, e)
	}
}
func TestRapidReleasePrepareDoesNotReuseCanceledJob(t *testing.T) {
	p := fixture()
	finishCancel := make(chan struct{})
	observedCancel := make(chan struct{})
	var calls atomic.Int32
	m := newTest(t, t.TempDir(), func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			_, _ = pw.Write(p[:32768])
			<-r.Context().Done()
			close(observedCancel)
			<-finishCancel
		}()
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: pr, Header: make(http.Header)}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	first := make([]byte, 32)
	if _, e := io.ReadFull(r.Body, first); e != nil {
		t.Fatal(e)
	}
	m.Release(s.CacheToken)
	<-observedCancel
	next, e := m.Prepare(context.Background(), candidate())
	if e != nil || next.URL != candidate().URL || next.CacheToken != "" {
		t.Fatal("reused canceled job", next, e)
	}
	close(finishCancel)
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	retry, e := m.Prepare(context.Background(), candidate())
	if e != nil || retry.CacheToken == "" {
		t.Fatal("job cleanup blocked later retry", retry, e)
	}
	m.Release(retry.CacheToken)
	if calls.Load() != 1 {
		t.Fatal("duplicate download", calls.Load())
	}
}
func TestUnstartedReleasedJobUsesFreshSource(t *testing.T) {
	p := fixture()
	var upstream string
	m := newTest(t, t.TempDir(), func(r *http.Request) (*http.Response, error) {
		upstream = r.URL.Path
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	old := candidate()
	old.URL = "https://m7.music.126.net/old.mp3"
	s, _ := m.Prepare(context.Background(), old)
	m.Release(s.CacheToken)
	fresh := candidate()
	fresh.URL = "https://m7.music.126.net/fresh.mp3"
	next, e := m.Prepare(context.Background(), fresh)
	if e != nil {
		t.Fatal(e)
	}
	r := get(t, next.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	if upstream != "/fresh.mp3" {
		t.Fatal("stale source reused", upstream)
	}
}
func TestPublicFormatGuardRejectsMislabeledLossless(t *testing.T) {
	for _, format := range []string{"flac", "wav", "alac", "m4a"} {
		s := candidate()
		s.ActualFormat = format
		s.ActualBitrate = 128000
		if public(s) || allowed(s, "", domain.PlaybackEntitlement{}) {
			t.Fatal("public mislabeled lossless allowed", format)
		}
	}
}

type heldResponse struct {
	header          http.Header
	started, finish chan struct{}
	once            sync.Once
}

func (w *heldResponse) Header() http.Header { return w.header }
func (w *heldResponse) WriteHeader(int)     {}
func (w *heldResponse) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.finish
	return len(p), nil
}
func TestResponsePinRetainsFileAfterTokenRelease(t *testing.T) {
	p := fixture()
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	w := &heldResponse{header: make(http.Header), started: make(chan struct{}), finish: make(chan struct{})}
	req, _ := http.NewRequest("GET", s.URL, nil)
	done := make(chan struct{})
	go func() { defer close(done); m.serve(w, req) }()
	<-w.started
	c, e := m.Clear()
	if e != nil || c.RetainedActiveTracks != 1 {
		t.Fatal(c, e)
	}
	m.Release(s.CacheToken)
	if _, e = os.Stat(m.blob(s.CacheEntryID)); e != nil || m.Stats().UsedBytes != int64(len(p)) {
		t.Fatal("response lost file/accounting", e, m.Stats())
	}
	close(w.finish)
	<-done
	if _, e = os.Stat(m.blob(s.CacheEntryID)); !os.IsNotExist(e) || m.Stats().UsedBytes != 0 {
		t.Fatal("response pin not released", e, m.Stats())
	}
}
func TestNetworkSelectionReusesVerifiedFallbackWithCacheProvenance(t *testing.T) {
	p := make([]byte, 417*100)
	for off := 0; off < len(p); off += 417 {
		binary.BigEndian.PutUint32(p[off:], 0xfffb9000)
	}
	var calls atomic.Int32
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	source := candidate()
	source.ActualQuality = "standard"
	source.ActualBitrate = 128000
	source.RequestedQuality = "standard"
	s, _ := m.Prepare(context.Background(), source)
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	m.Release(s.CacheToken)
	source.RequestedQuality = "exhigh"
	source.URL = "https://m7.music.126.net/new-signed.mp3"
	hit, e := m.Prepare(context.Background(), source)
	if e != nil || !hit.CacheHit || hit.RequestedQuality != "exhigh" || hit.ActualQuality != "standard" || hit.AccessStrategy != "cache" || hit.CacheEntryID == "" {
		t.Fatal("lost verified cache provenance", hit, e)
	}
	r = get(t, hit.URL, "")
	b, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil || r.StatusCode != 200 || !bytes.Equal(b, p) || calls.Load() != 1 {
		t.Fatal("verified fallback fetched upstream", e, r.StatusCode, calls.Load())
	}
}
func TestNetworkSelectionReplacesCorruptExistingBlob(t *testing.T) {
	p := fixture()
	var calls atomic.Int32
	m := newTest(t, t.TempDir(), func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	m.Release(s.CacheToken)
	if e := os.WriteFile(m.blob(s.CacheEntryID), p[:16], 0600); e != nil {
		t.Fatal(e)
	}
	fresh := candidate()
	fresh.URL = "https://m7.music.126.net/fresh-valid.mp3"
	next, e := m.Prepare(context.Background(), fresh)
	if e != nil || next.CacheHit || next.CacheToken == "" {
		t.Fatal("corrupt cache blocked fresh source", next, e)
	}
	r = get(t, next.URL, "")
	b, e := io.ReadAll(r.Body)
	r.Body.Close()
	waitDone(t, m)
	if e != nil || r.StatusCode != 200 || !bytes.Equal(b, p) || calls.Load() != 2 {
		t.Fatal("corrupt cache produced failure instead of refetch", e, r.StatusCode, calls.Load())
	}
}
func TestNetworkSelectionCorruptLeasedBlobPreservesFreshDirectSource(t *testing.T) {
	p := fixture()
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	if e := os.WriteFile(m.blob(s.CacheEntryID), p[:16], 0600); e != nil {
		t.Fatal(e)
	}
	fresh := candidate()
	fresh.URL = "https://m7.music.126.net/fresh-direct.mp3"
	next, e := m.Prepare(context.Background(), fresh)
	if e != nil || next.URL != fresh.URL || next.CacheToken != "" || next.CacheHit {
		t.Fatal("leased invalid entry replaced valid direct URL", next, e)
	}
	if _, e = os.Stat(m.blob(s.CacheEntryID)); e != nil {
		t.Fatal("leased physical blob deleted", e)
	}
	if _, ok := m.Lookup(context.Background(), "123", "exhigh", domain.PlaybackEntitlement{}); ok {
		t.Fatal("invalid leased blob still discoverable")
	}
	m.Release(s.CacheToken)
	if _, e = os.Stat(m.blob(s.CacheEntryID)); !os.IsNotExist(e) {
		t.Fatal("invalid blob retained after final release", e)
	}
}
