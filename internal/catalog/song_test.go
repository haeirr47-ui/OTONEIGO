package catalog

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetSongCanonicalLargeIDAndOrdinaryBoundary(t *testing.T) {
	const id = "9007199254740993"
	calls := 0
	c := NewClientWithHTTP(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://music.163.com/weapi/v3/song/detail" || r.Header.Get("X-Real-IP") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("metadata request boundary")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200,"songs":[{"id":9007199254740993,"name":"song","dt":267232,"ar":[{"id":0,"name":"unknown"},{"id":9007199254740995,"name":"artist"}],"al":{"id":3,"name":"album","picUrl":"http://p1.music.126.net/cover.jpg"}}]}`)), Header: make(http.Header)}, nil
	})}, "https://music.163.com")
	v, e := c.GetSong(context.Background(), id)
	if e != nil || v.NeteaseSongID != id || v.DurationMs != 267232 || v.Artists[1].ID != "9007199254740995" || v.Artists[0].ID != "" || v.CoverRef != "https://p1.music.126.net/cover.jpg" {
		t.Fatalf("%+v %v", v, e)
	}
	for _, bad := range []string{"1e3", "1.0", "0", "123,456", ""} {
		if _, e := c.GetSong(context.Background(), bad); e == nil {
			t.Fatal("unsafe ID")
		}
	}
	if calls != 1 {
		t.Fatal("invalid ID reached network")
	}
}
func TestGetSongRejectsMismatchedAndInvalidMetadata(t *testing.T) {
	for _, body := range []string{`{"code":200,"songs":[]}`, `{"code":200,"songs":[{"id":456,"name":"song"}]}`, `{"code":200,"songs":[{"id":123,"name":"","dt":123}]}`, `{"code":200,"songs":[{"id":123,"name":"song","dt":86400001}]}`, `{"code":200,"songs":[{"id":123,"name":"song","ar":[{"id":1e3,"name":"artist"}]}]}`, `{"code":200,"songs":[{"id":123,"name":"song","al":{"id":1.5}}]}`} {
		v, e := fixture(t, body).GetSong(context.Background(), "123")
		if e == nil || v.NeteaseSongID != "" {
			t.Fatal("invalid metadata returned", v, e)
		}
	}
	for _, code := range []string{"301", "429"} {
		if _, e := fixture(t, `{"code":`+code+`}`).GetSong(context.Background(), "123"); e == nil {
			t.Fatal("provider failure ignored")
		}
	}
	v, e := fixture(t, `{"code":200,"songs":[{"id":123,"name":"song","dt":123,"al":{"id":0,"picUrl":"https://m7.music.126.net/audio.mp3?token=secret"}}]}`).GetSong(context.Background(), "123")
	if e != nil || v.CoverRef != "" {
		t.Fatal("media URL in canonical metadata", v, e)
	}
}
