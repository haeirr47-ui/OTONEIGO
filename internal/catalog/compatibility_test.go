package catalog

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCompatibilityHeaderIsolation(t *testing.T) {
	calls := 0
	h := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		hint := r.Header.Get("X-Real-IP")
		if calls == 1 {
			if hint != regionHint || r.Method != "POST" || r.URL.String() != "https://music.163.com/weapi/song/enhance/player/url/v1" {
				t.Fatal("hint outside approved request")
			}
		} else if hint != "" {
			t.Fatal("hint leaked")
		}
		if r.Header.Get("Cookie") != "" {
			t.Fatal("cookie leaked")
		}
		body := `{"code":200,"data":[{"id":123,"code":200,"url":"https://m7.music.126.net/a.mp3","freeTrialInfo":null,"time":123000,"br":128000,"size":4567890}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	c := NewClientWithHTTP(h, "https://music.163.com")
	v, e := c.ResolveCompatible(context.Background(), "123", "standard")
	if e != nil || v.AccessStrategy != "region-header" || v.ActualBitrate != 128000 || v.ContentLengthBytes != 4567890 {
		t.Fatalf("%+v %v", v, e)
	}
	_, _ = c.Resolve(context.Background(), "123", "standard")
	_, _ = c.Search(context.Background(), "x", 0)
	_, _ = c.Lyrics(context.Background(), "123")
	if calls != 4 {
		t.Fatal(calls)
	}
	for _, base := range []string{"http://music.163.com", "https://music.163.com:443", "https://music.163.com.evil.example", "https://user@music.163.com"} {
		_, e := NewClientWithHTTP(h, base).ResolveCompatible(context.Background(), "123", "standard")
		if e == nil || e.Error() != "COMPATIBILITY_ENDPOINT_REJECTED" {
			t.Fatal("unsafe endpoint accepted", base, e)
		}
	}
	if calls != 4 {
		t.Fatal("unsafe network request occurred")
	}
}
func TestItemRateLimitAndAuthorization(t *testing.T) {
	for _, code := range []string{"301", "429", "403", "-110"} {
		v, e := fixture(t, `{"code":200,"data":[{"id":123,"code":`+code+`,"url":""}]}`).Resolve(context.Background(), "123", "standard")
		if code == "301" || code == "429" {
			if e == nil {
				t.Fatal("missing protected failure")
			}
		} else if e != nil || v.Status != "unauthorized" {
			t.Fatalf("%+v %v", v, e)
		}
	}
}
func TestExplicitAuthorizationNeverReturnsMedia(t *testing.T) {
	for _, code := range []string{"401", "403", "-110"} {
		v, e := fixture(t, `{"code":200,"data":[{"id":123,"code":`+code+`,"url":"https://m7.music.126.net/a.mp3","freeTrialInfo":null,"time":123}]}`).Resolve(context.Background(), "123", "standard")
		if e != nil || v.Status != "unauthorized" || v.URL != "" {
			t.Fatal("unauthorized item exposed media", v, e)
		}
	}
}
