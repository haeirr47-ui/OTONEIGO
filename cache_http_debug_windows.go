//go:build windows && p0debug

package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Only the explicit acceptance build can throttle media to establish that
// native playback starts before a cache download reaches EOF.
func cacheMediaHTTPClient() *http.Client {
	ms, err := strconv.Atoi(os.Getenv("OTONEIGO_P0_CACHE_DELAY_MS"))
	if err != nil || ms < 1 || ms > 200 {
		return nil
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.MaxResponseHeaderBytes = 32 * 1024
	return &http.Client{Transport: cacheTestTransport{base, time.Duration(ms) * time.Millisecond}, Timeout: 4 * time.Minute}
}

type cacheTestTransport struct {
	base  http.RoundTripper
	delay time.Duration
}

func (t cacheTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err == nil {
		response.Body = &cacheTestBody{response.Body, r.Context(), t.delay}
	}
	return response, err
}

type cacheTestBody struct {
	io.ReadCloser
	ctx   context.Context
	delay time.Duration
}

func (b *cacheTestBody) Read(p []byte) (int, error) {
	timer := time.NewTimer(b.delay)
	defer timer.Stop()
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-timer.C:
	}
	if len(p) > 32*1024 {
		p = p[:32*1024]
	}
	return b.ReadCloser.Read(p)
}
