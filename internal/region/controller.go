package region

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/voici5986/otoneigo/internal/crashreport"
	"github.com/voici5986/otoneigo/internal/domain"
)

const traceURL = "https://www.cloudflare.com/cdn-cgi/trace"

type Options struct {
	HTTP     *http.Client
	Now      func() time.Time
	TTL      time.Duration
	MinRetry time.Duration
	// Monitor reports interface/route changes and must release registrations on ctx cancellation.
	Monitor func(context.Context, func()) error
}

type Controller struct {
	monitorDone   sync.WaitGroup
	mu            sync.Mutex
	status        domain.RegionStatus
	changes       chan struct{}
	pending       chan struct{}
	pendingCancel context.CancelFunc
	lastAttempt   time.Time
	options       Options
	client        *http.Client
	ctx           context.Context
	cancel        context.CancelFunc
	started       bool
	closed        bool
}

func NewController() *Controller { return NewControllerWithOptions(Options{}) }
func NewControllerWithOptions(o Options) *Controller {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.TTL <= 0 {
		o.TTL = 15 * time.Minute
	}
	if o.MinRetry <= 0 {
		o.MinRetry = 30 * time.Second
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{}
	}
	if o.Monitor == nil {
		o.Monitor = monitorNetwork
	}
	h := *o.HTTP
	h.Jar = nil
	h.Timeout = 4 * time.Second
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("REGION_REDIRECT_REJECTED") }
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{status: domain.RegionStatus{Classification: "unknown"}, changes: make(chan struct{}), options: o, client: &h, ctx: ctx, cancel: cancel}
}
func (c *Controller) Status() domain.RegionStatus { c.mu.Lock(); defer c.mu.Unlock(); return c.status }

// Changes closes on a network generation change; callers obtain a fresh channel afterwards.
func (c *Controller) Changes() <-chan struct{} { c.mu.Lock(); defer c.mu.Unlock(); return c.changes }
func (c *Controller) Refresh(ctx context.Context) (domain.RegionStatus, error) {
	if ctx.Err() != nil {
		return c.Status(), ctx.Err()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.Status(), context.Canceled
	}
	if c.pending != nil {
		done := c.pending
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return c.Status(), ctx.Err()
		case <-done:
			s := c.Status()
			if s.ErrorCode != "" {
				return s, errors.New("REGION_DETECTION_FAILED")
			}
			return s, nil
		}
	}
	now := c.options.Now()
	if !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < c.options.MinRetry {
		s := c.status
		c.mu.Unlock()
		return s, nil
	}
	c.lastAttempt = now
	done := make(chan struct{})
	c.pending = done
	c.status.Detecting = true
	gen := c.status.NetworkGeneration
	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	c.pendingCancel = cancel
	c.mu.Unlock()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	var country string
	var err error
	// Detection owns no controller locks or shared-state mutations.
	if crashreport.Contain("region-detect", func() { country, err = c.detect(requestCtx) }) {
		country, err = "", errors.New("REGION_DETECTION_FAILED")
	}
	c.mu.Lock()
	if !c.closed && gen == c.status.NetworkGeneration {
		c.status.Detecting = false
		c.status.ObservedAt = c.options.Now().UnixMilli()
		c.status.Country = country
		c.status.ErrorCode = ""
		c.status.Classification = "unknown"
		if err != nil {
			c.status.ErrorCode = "REGION_DETECTION_FAILED"
		} else if country == "CN" {
			c.status.Classification = "CN"
		} else {
			c.status.Classification = "overseas"
		}
	}
	c.pending = nil
	c.pendingCancel = nil
	c.status.Detecting = false
	close(done)
	s := c.status
	c.mu.Unlock()
	return s, err
}
func (c *Controller) detect(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
	if err != nil {
		return "", errors.New("REGION_DETECTION_FAILED")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", errors.New("REGION_DETECTION_FAILED")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("REGION_DETECTION_FAILED")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return "", errors.New("REGION_DETECTION_FAILED")
	}
	country := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "loc=") {
			if country != "" {
				return "", errors.New("REGION_DETECTION_FAILED")
			}
			country = strings.TrimSpace(strings.TrimPrefix(line, "loc="))
		}
	}
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' || country == "XX" {
		return "", errors.New("REGION_DETECTION_FAILED")
	}
	return country, nil
}
func (c *Controller) networkChanged() {
	defer crashreport.Fatal("region-network")
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.status.NetworkGeneration++
	c.status.Classification = "unknown"
	c.status.Country = ""
	c.status.ObservedAt = 0
	c.status.ErrorCode = ""
	c.lastAttempt = time.Time{}
	pending := c.pending
	if c.pendingCancel != nil {
		c.pendingCancel()
	}
	close(c.changes)
	c.changes = make(chan struct{})
	c.mu.Unlock()
	go func() {
		defer crashreport.Fatal("region-refresh")
		if pending != nil {
			select {
			case <-pending:
			case <-c.ctx.Done():
				return
			}
		}
		_, _ = c.Refresh(c.ctx)
	}()
}

// Start never blocks application startup or ordinary resolution.
func (c *Controller) Start(ctx context.Context) {
	c.mu.Lock()
	if c.started || c.closed {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.monitorDone.Add(1)
	c.mu.Unlock()
	context.AfterFunc(ctx, func() {
		defer crashreport.Fatal("region-loop")
		c.Close()
	})
	go func() {
		defer crashreport.Fatal("region-refresh")
		_, _ = c.Refresh(c.ctx)
	}()
	go func() {
		defer crashreport.Fatal("region-monitor")
		defer c.monitorDone.Done()
		// A failed monitor retires after its own cleanup; polling remains active.
		crashreport.Contain("region-monitor", func() { _ = c.options.Monitor(c.ctx, c.networkChanged) })
	}()
	go func() {
		defer crashreport.Fatal("region-loop")
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				if c.refreshDue() {
					_, _ = c.Refresh(c.ctx)
				}
			}
		}
	}()
}
func (c *Controller) refreshDue() bool {
	s := c.Status()
	return s.Classification == "unknown" || s.ObservedAt == 0 || c.options.Now().Sub(time.UnixMilli(s.ObservedAt)) >= c.options.TTL
}

func (c *Controller) Close() {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
		close(c.changes)
	}
	c.mu.Unlock()
	c.monitorDone.Wait()
}
