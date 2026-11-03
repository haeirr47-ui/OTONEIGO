package region

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type waitingContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *waitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}
func awaitRegion[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("region work did not settle")
		var zero T
		return zero
	}
}
func TestDetectionPanicSettlesOwnerAndWaiter(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var ticks atomic.Int64
	c := NewControllerWithOptions(Options{Now: func() time.Time { return time.Unix(ticks.Add(1), 0) }, MinRetry: time.Nanosecond, HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			panic("token=private https://secret.invalid")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("loc=TH")), Header: make(http.Header)}, nil
	})}})
	defer c.Close()
	owner, waiter := make(chan error, 1), make(chan error, 1)
	go func() { _, err := c.Refresh(context.Background()); owner <- err }()
	awaitRegion(t, entered)
	ctx := &waitingContext{Context: context.Background(), entered: make(chan struct{})}
	go func() { _, err := c.Refresh(ctx); waiter <- err }()
	awaitRegion(t, ctx.entered)
	close(release)
	for _, ch := range []<-chan error{owner, waiter} {
		if err := awaitRegion(t, ch); err == nil || err.Error() != "REGION_DETECTION_FAILED" {
			t.Fatalf("non-generic error: %v", err)
		}
	}
	c.mu.Lock()
	settled := c.pending == nil && c.pendingCancel == nil && !c.status.Detecting
	c.mu.Unlock()
	if !settled {
		t.Fatal("pending detection retained")
	}
	if s, err := c.Refresh(context.Background()); err != nil || s.Country != "TH" {
		t.Fatal(s, err)
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	awaitRegion(t, closed)
}
func TestMonitorPanicRetiresAndCloseCompletes(t *testing.T) {
	cleaned := make(chan struct{})
	c := NewControllerWithOptions(Options{Monitor: func(context.Context, func()) error {
		defer close(cleaned)
		panic("cookie=private")
	}, HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("loc=CN")), Header: make(http.Header)}, nil
	})}})
	c.Start(context.Background())
	awaitRegion(t, cleaned)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	awaitRegion(t, closed)
}
