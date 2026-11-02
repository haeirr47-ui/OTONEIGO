package region

import (
	"context"
	"github.com/voici5986/otoneigo/internal/domain"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCountryCacheRetryAndIsolation(t *testing.T) {
	now := time.Now()
	calls := 0
	c := NewControllerWithOptions(Options{Now: func() time.Time { return now }, HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != traceURL || r.Method != "GET" || r.Header.Get("X-Real-IP") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("detection boundary")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ip=192.0.2.1\nloc=TH\n")), Header: make(http.Header)}, nil
	})}})
	defer c.Close()
	s, e := c.Refresh(context.Background())
	if e != nil || s.Classification != "overseas" || s.Country != "TH" {
		t.Fatal(s, e)
	}
	_, _ = c.Refresh(context.Background())
	if calls != 1 {
		t.Fatal("retry not coalesced")
	}
	// Explicit refresh can re-observe after min retry; automatic TTL refresh uses observed time.
	now = now.Add(31 * time.Second)
	_, _ = c.Refresh(context.Background())
	if calls != 2 {
		t.Fatal(calls)
	}
	if c.options.TTL != 15*time.Minute {
		t.Fatal("unexpected TTL")
	}
}
func TestDetectionFailureUnknownAndBound(t *testing.T) {
	for _, body := range []string{"loc=XX", "loc=TH\nloc=CN", "ip=192.0.2.1", strings.Repeat("x", 16385)} {
		c := NewControllerWithOptions(Options{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}})
		s, e := c.Refresh(context.Background())
		c.Close()
		if e == nil || s.Classification != "unknown" || s.Country != "" || s.ErrorCode != "REGION_DETECTION_FAILED" {
			t.Fatal(s, e)
		}
	}
}
func TestNetworkGenerationDiscardsLateCountry(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := NewControllerWithOptions(Options{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("loc=CN")), Header: make(http.Header)}, nil
	})}})
	defer c.Close()
	done := make(chan domain.RegionStatus, 1)
	go func() { s, _ := c.Refresh(context.Background()); done <- s }()
	<-started
	changes := c.Changes()
	c.networkChanged()
	select {
	case <-changes:
	default:
		t.Fatal("generation did not close")
	}
	close(release)
	s := <-done
	if s.Classification != "unknown" || s.NetworkGeneration != 1 {
		t.Fatal("stale country accepted", s)
	}
}
func TestCountryTTLAndFailureRetryEligibility(t *testing.T) {
	now := time.Now()
	c := NewControllerWithOptions(Options{Now: func() time.Time { return now }})
	defer c.Close()
	c.status = domain.RegionStatus{Classification: "overseas", Country: "TH", ObservedAt: now.UnixMilli()}
	now = now.Add(14 * time.Minute)
	if c.refreshDue() {
		t.Fatal("cache expired too early")
	}
	now = now.Add(time.Minute)
	if !c.refreshDue() {
		t.Fatal("TTL did not expire")
	}
	c.status = domain.RegionStatus{Classification: "unknown", ObservedAt: now.UnixMilli(), ErrorCode: "REGION_DETECTION_FAILED"}
	if !c.refreshDue() {
		t.Fatal("unknown must be retried after minimum interval")
	}
}
func TestCloseWaitsForMonitorCleanup(t *testing.T) {
	entered := make(chan struct{})
	cleaned := make(chan struct{})
	c := NewControllerWithOptions(Options{HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("loc=TH")), Header: make(http.Header)}, nil
	})}, Monitor: func(ctx context.Context, _ func()) error { close(entered); <-ctx.Done(); close(cleaned); return nil }})
	c.Start(context.Background())
	<-entered
	c.Close()
	select {
	case <-cleaned:
	default:
		t.Fatal("monitor not cleaned")
	}
	c.Close()
}
