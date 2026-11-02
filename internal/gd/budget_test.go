package gd

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/stream"
)

func TestGDRejectsInsufficientBudgetBeforeAPISlot(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	f.c.next = f.c.now().Add(14 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 17*time.Second)
	defer cancel()
	// Exercise the real new-flight path: a 14s rate wait leaves only 3s for the
	// four-second useful-execution admission floor.
	done := make(chan error, 1)
	go func() { _, err := f.c.Resolve(ctx, "33894312", "exhigh"); done <- err }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "GD_BUDGET_EXHAUSTED" {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("started an unusable rate wait instead of rejecting budget")
	}
	if f.calls.Load() != 0 || !f.c.next.Equal(f.c.now().Add(14*time.Second)) {
		t.Fatal("budget rejection consumed API slot")
	}
	if f.c.failures != 0 || !f.c.coolUntil.IsZero() {
		t.Fatal("local budget rejection counted as provider outage")
	}
}

func TestGDReusesVerifiedMetadataAcrossQualityLadder(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	calls := 0
	f.c.metadata = metadataFunc(func(_ context.Context, id string) (domain.TrackSnapshot, error) {
		calls++
		return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
	})
	for _, q := range []string{"exhigh", "higher", "standard"} {
		f.now.Add(int64(6 * time.Second))
		if _, err := f.c.Resolve(context.Background(), "33894312", q); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("same-ID quality ladder fetched metadata %d times", calls)
	}
	f.c.Invalidate()
	f.now.Add(int64(6 * time.Second))
	if _, err := f.c.Resolve(context.Background(), "33894312", "standard"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("invalidated metadata reused", calls)
	}
}

func TestGDRealResolverThirdTierFitsSharedBudget(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	f.c.now = time.Now
	metadataCalls := 0
	f.c.metadata = metadataFunc(func(_ context.Context, id string) (domain.TrackSnapshot, error) {
		metadataCalls++
		return domain.TrackSnapshot{NeteaseSongID: id, DurationMs: 267232}, nil
	})
	original := f.c.api.Transport
	var dispatched []time.Time
	f.c.api.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "music-api.gdstudio.xyz" {
			return original.RoundTrip(req)
		}
		f.c.mu.Lock()
		dispatched = append(dispatched, f.c.next.Add(-f.c.interval))
		f.c.mu.Unlock()
		br := req.URL.Query().Get("br")
		if br != "128" {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"url":"","br":` + br + `}`)), Request: req}, nil
		}
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Error("shared API HTTP cap extended")
		}
		return original.RoundTrip(req)
	})
	r := stream.NewResolver(unavailableCatalog{}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off", GDEnabled: true} })
	r.SetFallback(f.c)
	ctx, cancel := context.WithTimeout(context.Background(), 17*time.Second)
	defer cancel()
	got, err := r.Resolve(ctx, "33894312", "exhigh")
	if err != nil || got.ActualBitrate != 128000 || got.RequestedQuality != "exhigh" || len(dispatched) != 3 || metadataCalls != 1 {
		t.Fatal(got, err, len(dispatched), metadataCalls)
	}
	for i := 1; i < len(dispatched); i++ {
		if dispatched[i].Sub(dispatched[i-1]) < 6*time.Second {
			t.Fatal("minimum rate spacing lost")
		}
	}
	deadline, _ := ctx.Deadline()
	if got.ResolutionDeadlineAt != deadline.UnixMilli() {
		t.Fatal("caller total deadline replaced", got.ResolutionDeadlineAt, deadline)
	}
}

func TestGDShortFirstBudgetDoesNotShortenLaterCoalescedCaller(t *testing.T) {
	sample, total := audioPrefix(128000)
	f := fixtureClient(t, "https://m701.music.126.net/full.mp3", "128", sample, total, nil)
	started := make(chan struct{})
	original := f.c.api.Transport
	f.c.api.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-timer.C:
		}
		return original.RoundTrip(req)
	})
	short, cancel := context.WithTimeout(context.Background(), 4100*time.Millisecond)
	defer cancel()
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := f.c.Resolve(short, "33894312", "exhigh"); first <- err }()
	<-started
	go func() { _, err := f.c.Resolve(context.Background(), "33894312", "exhigh"); second <- err }()
	timeout := time.Now().Add(time.Second)
	for {
		f.c.mu.Lock()
		shared := f.c.flights["33894312:exhigh"]
		attached := shared != nil && shared.waiters == 2
		f.c.mu.Unlock()
		if attached {
			break
		}
		if time.Now().After(timeout) {
			t.Fatal("long caller did not join")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-first; err != context.Canceled {
		t.Fatal("first caller cancellation lost", err)
	}
	if err := <-second; err != nil {
		t.Fatal("short first budget killed long joined caller", err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("coalesced callers caused extra API requests", f.calls.Load())
	}
}
