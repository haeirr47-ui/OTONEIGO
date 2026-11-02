package gd

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/stream"
)

type metadataFunc func(context.Context, string) (domain.TrackSnapshot, error)

func (f metadataFunc) GetSong(ctx context.Context, id string) (domain.TrackSnapshot, error) {
	return f(ctx, id)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func audioPrefix(bits int) ([]byte, int64) {
	bi := 9
	if bits == 192000 {
		bi = 11
	}
	if bits == 320000 {
		bi = 14
	}
	n := 144 * bits / 44100
	sample := make([]byte, n*100)
	for i := 0; i < len(sample); i += n {
		binary.BigEndian.PutUint32(sample[i:i+4], 0xfffb0000|uint32(bi)<<12)
	}
	return sample, int64(bits) * 267232 / 8000
}

type fixture struct {
	c        *Client
	calls    atomic.Int32
	requests atomic.Int32
	now      atomic.Int64
}

func fixtureClient(t *testing.T, mediaURL string, reportedBR string, sample []byte, total int64, mutate func(*http.Request, http.ResponseWriter)) *fixture {
	t.Helper()
	f := &fixture{}
	f.now.Store(time.Now().UnixNano())
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		for _, h := range []string{"Cookie", "Authorization", "X-Real-IP", "X-Forwarded-For"} {
			if r.Header.Get(h) != "" {
				t.Errorf("leaked %s", h)
			}
		}
		if mutate != nil {
			mutate(r, w)
		}
		if r.Host == "music-api.gdstudio.xyz" {
			f.calls.Add(1)
			if r.URL.Path != "/api.php" || r.URL.Query().Get("types") != "url" || r.URL.Query().Get("source") != "netease" || r.URL.Query().Get("id") != "33894312" {
				t.Error("incorrect API request")
			}
			fmt.Fprintf(w, `{"url":%q,"br":%s,"size":1}`, mediaURL, reportedBR)
			return
		}
		if r.Header.Get("Range") != "bytes=0-65535" {
			t.Error("unbounded probe")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(sample)-1, total))
		w.Header().Set("Content-Length", fmt.Sprint(len(sample)))
		w.WriteHeader(206)
		_, _ = w.Write(sample)
	}))
	t.Cleanup(server.Close)
	base := server.Client().Transport
	f.c = NewClientWithOptions(metadataFunc(func(_ context.Context, id string) (domain.TrackSnapshot, error) {
		return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
	}), Options{Now: func() time.Time { return time.Unix(0, f.now.Load()) }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		clone.Host = r.URL.Host
		clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
		return base.RoundTrip(clone)
	})})
	t.Cleanup(f.c.Close)
	return f
}
func TestVerifiedFullAndHonestQuality(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "999", sample, total, nil)
	got, err := f.c.Resolve(context.Background(), "33894312", "hires")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "gd-netease" || got.Status != "full" || got.ActualBitrate != 128000 || got.ActualQuality != "standard" || got.DurationMs != 267232 || got.ActualFormat != "mp3" {
		t.Fatalf("incorrect measured fields: %+v", got)
	}
	_, err = f.c.Resolve(context.Background(), "33894312", "hires")
	if err != nil || f.calls.Load() != 1 {
		t.Fatal("cache missed", err)
	}
	f.now.Add(int64(31 * time.Second))
	_, err = f.c.Resolve(context.Background(), "33894312", "hires")
	if err != nil || f.calls.Load() != 2 {
		t.Fatal("expired cache did not refresh", err)
	}
}
func TestRejectUnverifiedMedia(t *testing.T) {
	sample, total := audioPrefix(128000)
	for _, tc := range []struct {
		name, url, br string
		data          []byte
		size          int64
	}{
		{"trial", "https://m701.music.126.net/trial.mp3", "128", sample, 30 * 16000},
		{"html", "https://m701.music.126.net/error", "128", []byte("<html>not audio</html>"), total},
		{"flac", "https://m701.music.126.net/lossless", "999", append([]byte("fLaC"), sample[4:]...), total},
		{"missing length", "https://m701.music.126.net/full.mp3", "128", sample, 0},
		{"oversized", "https://m701.music.126.net/full.mp3", "128", sample, maxMediaSize + 1},
		{"bad bitrate", "https://m701.music.126.net/full.mp3", "0", sample, total},
		{"credentials", "https://user:pass@m701.music.126.net/full.mp3", "128", sample, total},
		{"other catalog", "https://evil.example/full.mp3", "128", sample, total},
		{"http", "http://m701.music.126.net/full.mp3", "128", sample, total},
		{"port", "https://m701.music.126.net:443/full.mp3", "128", sample, total},
		{"fragment", "https://m701.music.126.net/full.mp3#x", "128", sample, total},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureClient(t, tc.url, tc.br, tc.data, tc.size, nil)
			if _, err := f.c.Resolve(context.Background(), "33894312", "standard"); err == nil {
				t.Fatal("accepted unverified media")
			}
		})
	}
}
func TestMP3Verification(t *testing.T) {
	sample, total := audioPrefix(128000)
	t.Run("id3", func(t *testing.T) {
		tag := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
		duration, bits, ok := mp3(append(tag, sample...), total+10)
		if !ok || duration != 267232 || bits != 128000 {
			t.Fatal(duration, bits, ok)
		}
	})
	t.Run("xing", func(t *testing.T) {
		data := append([]byte(nil), sample...)
		copy(data[36:], "Xing")
		if _, _, ok := mp3(data, total); ok {
			t.Fatal("accepted VBR")
		}
	})
	t.Run("info", func(t *testing.T) {
		data := append([]byte(nil), sample...)
		copy(data[36:], "Info")
		binary.BigEndian.PutUint32(data[40:], 1)
		binary.BigEndian.PutUint32(data[44:], 10230)
		if _, _, ok := mp3(data, total); !ok {
			t.Fatal("rejected consistent CBR Info")
		}
	})
	t.Run("inconsistent frames", func(t *testing.T) {
		data := append([]byte(nil), sample...)
		data[417+2] = 0xb0
		if _, _, ok := mp3(data, total); ok {
			t.Fatal("accepted inconsistent frames")
		}
	})
	t.Run("huge id3", func(t *testing.T) {
		data := append([]byte(nil), sample...)
		copy(data, []byte{'I', 'D', '3', 4, 0, 0, 127, 127, 127, 127})
		if _, _, ok := mp3(data, total); ok {
			t.Fatal("accepted huge ID3")
		}
	})
}
func TestRedirectsNeverEscapeAllowlist(t *testing.T) {
	sample, total := audioPrefix(128000)
	for _, api := range []bool{true, false} {
		t.Run(fmt.Sprint(api), func(t *testing.T) {
			f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, func(r *http.Request, w http.ResponseWriter) {
				if (r.Host == "music-api.gdstudio.xyz") == api {
					w.Header().Set("Location", "https://evil.example/secret")
					w.WriteHeader(302)
				}
			})
			if _, err := f.c.Resolve(context.Background(), "33894312", "standard"); err == nil {
				t.Fatal("accepted redirect")
			}
			want := int32(2)
			if api {
				want = 1
			}
			if f.requests.Load() != want {
				t.Fatal("followed untrusted redirect")
			}
		})
	}
}
func TestCancellationCoalescingAndGeneration(t *testing.T) {
	sample, total := audioPrefix(128000)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, func(r *http.Request, w http.ResponseWriter) {
		if r.Host == "music-api.gdstudio.xyz" {
			once.Do(func() { close(started) })
			<-release
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { _, err := f.c.Resolve(ctx, "33894312", "standard"); first <- err }()
	<-started
	go func() { _, err := f.c.Resolve(context.Background(), "33894312", "standard"); second <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		f.c.mu.Lock()
		n := f.c.flights["33894312:standard"].waiters
		f.c.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter not attached")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if <-first != context.Canceled {
		t.Fatal("caller cancellation lost")
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("not coalesced")
	}
	f.c.Invalidate()
	f.c.mu.Lock()
	n := len(f.c.cache)
	f.c.mu.Unlock()
	if n != 0 {
		t.Fatal("cache survived invalidation")
	}
}
func TestRateWaitAndLastWaiterCancellation(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	if _, err := f.c.Resolve(context.Background(), "33894312", "standard"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := f.c.Resolve(ctx, "33894312", "higher"); err != errBudgetExhausted {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second || f.calls.Load() != 1 {
		t.Fatal("rate wait escaped deadline or dispatched")
	}
}
func TestInvalidateCancelsInFlightAndDropsStaleResult(t *testing.T) {
	started := make(chan struct{})
	done := make(chan error, 1)
	c := NewClient(metadataFunc(func(ctx context.Context, id string) (domain.TrackSnapshot, error) {
		close(started)
		<-ctx.Done()
		return domain.TrackSnapshot{}, ctx.Err()
	}))
	defer c.Close()
	go func() { _, err := c.Resolve(context.Background(), "33894312", "standard"); done <- err }()
	<-started
	c.Invalidate()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("inflight not cancelled")
	}
}
func TestMalformedAPIAndRange(t *testing.T) {
	for _, tc := range []struct{ name, body, rangeHeader string }{
		{"bad json", "{", ""}, {"oversized json", strings.Repeat("x", 16385), ""},
		{"wrong range start", `{"url":"https://m701.music.126.net/full.mp3","br":128}`, "bytes 1-100/4275712"},
		{"range unknown total", `{"url":"https://m701.music.126.net/full.mp3","br":128}`, "bytes 0-100/*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClientWithOptions(metadataFunc(func(_ context.Context, id string) (domain.TrackSnapshot, error) {
				return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
			}), Options{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := tc.body
				status := 200
				h := make(http.Header)
				if r.URL.Host != "music-api.gdstudio.xyz" {
					body = strings.Repeat("x", 101)
					status = 206
					h.Set("Content-Range", tc.rangeHeader)
				}
				return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
			})})
			defer c.Close()
			if _, err := c.Resolve(context.Background(), "33894312", "standard"); err == nil {
				t.Fatal("accepted malformed response")
			}
		})
	}
}

func TestLastWaiterCancelsMetadata(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	c := NewClient(metadataFunc(func(ctx context.Context, id string) (domain.TrackSnapshot, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return domain.TrackSnapshot{}, ctx.Err()
	}))
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Resolve(ctx, "33894312", "standard"); done <- err }()
	<-started
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("last waiter did not cancel provider")
	}
}
func TestCacheIsPerRequestedQualityAndGeneration(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	if _, err := f.c.Resolve(context.Background(), "33894312", "standard"); err != nil {
		t.Fatal(err)
	}
	f.now.Add(int64(6 * time.Second))
	if _, err := f.c.Resolve(context.Background(), "33894312", "higher"); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 2 {
		t.Fatal("reused cache for different quality")
	}
	f.c.Invalidate()
	f.now.Add(int64(6 * time.Second))
	if _, err := f.c.Resolve(context.Background(), "33894312", "standard"); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 3 {
		t.Fatal("reused invalidated cache")
	}
}
func TestCircuitCooldownAndValidationDoNotDispatch(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://evil.example/full.mp3", "128", sample, total, nil)
	for _, q := range []string{"standard", "higher", "exhigh"} {
		f.now.Add(int64(6 * time.Second))
		if _, err := f.c.Resolve(context.Background(), "33894312", q); err == nil {
			t.Fatal("accepted bad host")
		}
	}
	if _, err := f.c.Resolve(context.Background(), "33894312", "hires"); err == nil || err.Error() != "GD_RATE_LIMITED" {
		t.Fatal("cooldown missing", err)
	}
	if _, err := f.c.Resolve(context.Background(), "01", "standard"); err == nil {
		t.Fatal("accepted invalid ID")
	}
	if _, err := f.c.Resolve(context.Background(), "33894312", "bad"); err == nil {
		t.Fatal("accepted invalid quality")
	}
	if f.calls.Load() != 3 {
		t.Fatal("invalid input dispatched")
	}
}

// A fake clock advances between dispatches: no six-second wall-clock sleeps.
func TestQualityRetriesRespectRateAndHaveRoomToWait(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	var dispatchTimes []time.Time
	var qualities []string
	original := f.c.api.Transport
	f.c.api.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		dispatchTimes = append(dispatchTimes, f.c.now())
		qualities = append(qualities, r.URL.Query().Get("br"))
		f.now.Add(int64(6 * time.Second))
		return original.RoundTrip(r)
	})
	f.c.metadata = metadataFunc(func(ctx context.Context, id string) (domain.TrackSnapshot, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > requestTimeout {
			t.Error("metadata request is not bounded independently")
		}
		return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 17*time.Second)
	defer cancel()
	for _, q := range []string{"exhigh", "higher", "standard"} {
		if _, err := f.c.Resolve(ctx, "33894312", q); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(qualities, ",") != "320,192,128" || len(dispatchTimes) != 3 {
		t.Fatal(qualities)
	}
	for i := 1; i < len(dispatchTimes); i++ {
		if dispatchTimes[i].Sub(dispatchTimes[i-1]) < 6*time.Second {
			t.Fatal("rate minimum violated")
		}
	}
}
func TestCanceledRateWaitDoesNotConsumeSlot(t *testing.T) {
	c := NewClient(nil)
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.rate(ctx); err != context.Canceled || !c.next.IsZero() {
		t.Fatal(err, c.next)
	}
}

type unavailableCatalog struct{}

func (unavailableCatalog) Resolve(context.Context, string, string) (domain.ResolvedStream, error) {
	return domain.ResolvedStream{Status: "unavailable"}, nil
}
func (unavailableCatalog) ResolveCompatible(context.Context, string, string) (domain.ResolvedStream, error) {
	return domain.ResolvedStream{Status: "unavailable"}, nil
}
func TestMemberLadderQualityMissesDoNotTripServiceCooldown(t *testing.T) {
	sample, total := audioPrefix(128000)
	now := time.Now()
	var got []string
	c := NewClientWithOptions(metadataFunc(func(ctx context.Context, id string) (domain.TrackSnapshot, error) {
		return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
	}), Options{Now: func() time.Time { return now }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := make(http.Header)
		status := 200
		var body []byte
		if r.URL.Host == "music-api.gdstudio.xyz" {
			now = now.Add(6 * time.Second)
			br := r.URL.Query().Get("br")
			got = append(got, br)
			media := "https://m701.music.126.net/" + br
			if br == "320" || br == "192" {
				media = ""
			}
			body = []byte(fmt.Sprintf(`{"url":%q,"br":%s}`, media, br))
		} else {
			body = append([]byte(nil), sample...)
			if r.URL.Path != "/128" {
				copy(body, "fLaC")
			}
			status = 206
			h.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(body)-1, total))
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(string(body))), ContentLength: int64(len(body)), Request: r}, nil
	})})
	defer c.Close()
	r := stream.NewResolver(unavailableCatalog{}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off", GDEnabled: true} }, stream.Options{PlaybackEntitlement: func() domain.PlaybackEntitlement { return domain.PlaybackEntitlement{LoggedIn: true, Member: true} }})
	r.SetFallback(c)
	v, err := r.Resolve(context.Background(), "33894312", "jymaster")
	if err != nil || strings.Join(got, ",") != "999,740,320,192,128" || v.ActualBitrate != 128000 || v.Status != "full" {
		t.Fatal(got, v, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures != 0 || !c.coolUntil.IsZero() {
		t.Fatal("quality misses triggered outage cooldown", c.failures, c.coolUntil)
	}
}
func TestShortFirstDeadlineDoesNotCancelLongCoalescedWaiter(t *testing.T) {
	sample, total := audioPrefix(128000)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, func(r *http.Request, w http.ResponseWriter) {
		if r.Host == "music-api.gdstudio.xyz" {
			once.Do(func() { close(started) })
			<-release
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	first := make(chan error, 1)
	second := make(chan error, 1)
	// A caller without enough remaining budget may join useful existing work,
	// but cannot start a new API request that has no verification reserve.
	go func() { _, err := f.c.Resolve(context.Background(), "33894312", "standard"); second <- err }()
	<-started
	go func() { _, err := f.c.Resolve(ctx, "33894312", "standard"); first <- err }()
	timeout := time.Now().Add(time.Second)
	for {
		f.c.mu.Lock()
		flight := f.c.flights["33894312:standard"]
		attached := flight != nil && flight.waiters == 2
		f.c.mu.Unlock()
		if attached {
			break
		}
		if time.Now().After(timeout) {
			close(release)
			t.Fatal("second waiter did not attach")
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-first; err != context.DeadlineExceeded {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal("short first deadline killed shared flight", err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("not coalesced", f.calls.Load())
	}
}

func TestNetworkFailuresStillTripGlobalCooldown(t *testing.T) {
	now := time.Now()
	calls := 0
	c := NewClientWithOptions(metadataFunc(func(ctx context.Context, id string) (domain.TrackSnapshot, error) {
		return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
	}), Options{Now: func() time.Time { return now }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("fixture network failure")
	})})
	defer c.Close()
	for _, q := range []string{"exhigh", "higher", "standard"} {
		now = now.Add(6 * time.Second)
		if _, err := c.Resolve(context.Background(), "33894312", q); err == nil || err.Error() != "GD_REQUEST_FAILED" {
			t.Fatal(err)
		}
	}
	if _, err := c.Resolve(context.Background(), "33894312", "hires"); err == nil || err.Error() != "GD_RATE_LIMITED" || calls != 3 {
		t.Fatal(err, calls)
	}
}
