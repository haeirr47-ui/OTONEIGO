package catalog

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasPrefix(r.URL.Path, "/weapi/") {
			t.Error("unexpected endpoint")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClientWithHTTP(srv.Client(), srv.URL)
}
func TestSearchLargeIDs(t *testing.T) {
	c := fixture(t, `{"code":200,"result":{"songs":[{"id":9007199254740993,"name":"song","dt":123456,"ar":[{"id":9007199254740995,"name":"artist"}],"al":{"id":123,"name":"album","picUrl":"http://p1.music.126.net/image.jpg"}}],"songCount":31}}`)
	r, e := c.Search(context.Background(), "test", 0)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Tracks) != 1 || r.Tracks[0].NeteaseSongID != "9007199254740993" || r.Tracks[0].Artists[0].ID != "9007199254740995" || !r.HasMore || !strings.HasPrefix(r.Tracks[0].CoverRef, "https:") {
		t.Fatalf("bad result %+v", r)
	}
}
func TestAnonymousUnknownArtist(t *testing.T) {
	c := fixture(t, `{"code":200,"result":{"songs":[{"id":123,"name":"song","dt":123,"ar":[{"id":0,"name":"unknown"}],"al":{"id":0}}],"songCount":1}}`)
	r, e := c.Search(context.Background(), "test", 0)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Tracks) != 1 || r.Tracks[0].Artists[0].ID != "" {
		t.Fatal("unknown upstream artist must remain optional")
	}
}
func TestResolveClassifications(t *testing.T) {
	for _, tc := range []struct{ name, extra, status string }{{"full", `,"freeTrialInfo":null,"time":120000`, "full"}, {"trial", `,"freeTrialInfo":{"start":0,"end":30},"time":30000`, "trial"}, {"unknown", ``, "unknown"}} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, `{"code":200,"data":[{"id":123,"code":200,"url":"http://m7.music.126.net/audio.mp3","level":"standard","type":"mp3"`+tc.extra+`}]}`)
			r, e := c.Resolve(context.Background(), "123", "standard")
			if e != nil {
				t.Fatal(e)
			}
			if r.Status != tc.status || r.URL != "https://m7.music.126.net/audio.mp3" {
				t.Fatalf("bad resolution status=%s", r.Status)
			}
		})
	}
}
func TestResolveRejectsMismatchAndHost(t *testing.T) {
	for _, body := range []string{`{"code":200,"data":[{"id":456,"code":200,"url":"https://m7.music.126.net/a"}]}`, `{"code":200,"data":[{"id":123,"code":200,"url":"https://music.126.net.evil.example/a"}]}`, `{"code":200,"data":[{"id":123,"code":200,"url":"https://u:p@m7.music.126.net/a"}]}`} {
		r, e := fixture(t, body).Resolve(context.Background(), "123", "standard")
		if e == nil || r.URL != "" {
			t.Fatal("unsafe response accepted")
		}
	}
}
func TestFailuresAndCancel(t *testing.T) {
	if _, e := fixture(t, `not json`).Search(context.Background(), "x", 0); e == nil {
		t.Fatal("invalid response accepted")
	}
	if _, e := fixture(t, `{"code":429}`).Search(context.Background(), "x", 0); e == nil || e.Error() != "RATE_LIMITED" {
		t.Fatal(e)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, e := NewClientWithHTTP(srv.Client(), srv.URL).Lyrics(ctx, "123")
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestCBCProtocolPreset(t *testing.T) {
	encoded := encryptCBC([]byte(`{"s":"test"}`), []byte("0CoJUm6Qyw8W8jud"))
	raw, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		t.Fatal(e)
	}
	block, _ := aes.NewCipher([]byte("0CoJUm6Qyw8W8jud"))
	cipher.NewCBCDecrypter(block, []byte("0102030405060708")).CryptBlocks(raw, raw)
	pad := int(raw[len(raw)-1])
	if string(raw[:len(raw)-pad]) != `{"s":"test"}` {
		t.Fatal("wrong protocol padding")
	}
}
