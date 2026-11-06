package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func discoverySong(id string) string {
	return `{"id":` + id + `,"name":"song","dt":100,"ar":[{"id":0,"name":"artist"}],"al":{"id":3,"name":"album","picUrl":"http://p1.music.126.net/c.jpg"}}`
}
func discoveryPlaylist(ids []string, tracks []string, total int) string {
	objects := make([]string, len(ids))
	for i, id := range ids {
		objects[i] = `{"id":` + id + `}`
	}
	return fmt.Sprintf(`{"code":200,"playlist":{"id":9007199254740993,"name":"list","description":"description","coverImgUrl":"http://p1.music.126.net/c.jpg","trackCount":%d,"trackIds":[%s],"tracks":[%s]}}`, total, strings.Join(objects, ","), strings.Join(tracks, ","))
}
func discoveryFixture(t *testing.T, detail, batch string) (*Client, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Real-IP") != "" {
			t.Error("anonymous boundary", r.Method, r.Header)
		}
		switch r.URL.Path {
		case "/weapi/v3/playlist/detail":
			io.WriteString(w, detail)
		case "/weapi/v3/song/detail":
			io.WriteString(w, batch)
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return NewClientWithHTTP(server.Client(), server.URL), calls
}
func TestDiscoveryPlaylistOrderedBoundedBatchAndLargeIDs(t *testing.T) {
	ids := []string{"9007199254740995", "9007199254740994", "9007199254740996"}
	client, calls := discoveryFixture(t, discoveryPlaylist(ids, []string{discoverySong(ids[0])}, 3), `{"code":200,"songs":[`+discoverySong(ids[2])+`,`+discoverySong(ids[1])+`]}`)
	result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0)
	if err != nil || result.Partial || result.HasMore || result.Total != 3 || result.AvailableCount != 3 || result.NextOffset != 3 || calls.Load() != 2 {
		t.Fatal(result, err, calls.Load())
	}
	for i, id := range ids {
		if result.Tracks[i].NeteaseSongID != id || result.Tracks[i].CoverRef != "https://p1.music.126.net/c.jpg" {
			t.Fatal(result.Tracks)
		}
	}
}
func TestDiscoveryPlaylistOffsetsAndMissingMetadata(t *testing.T) {
	ids := make([]string, 65)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	client, calls := discoveryFixture(t, discoveryPlaylist(ids, nil, 65), `{"code":200,"songs":[`+discoverySong("31")+`,`+discoverySong("60")+`]}`)
	result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 30)
	if err != nil || !result.Partial || !result.HasMore || result.Offset != 30 || result.NextOffset != 60 || len(result.Tracks) != 2 || calls.Load() != 2 {
		t.Fatal(result, err, calls.Load())
	}
	result, err = client.GetPublicPlaylist(context.Background(), "9007199254740993", 90)
	if err != nil || result.HasMore || result.NextOffset != 65 || len(result.Tracks) != 0 || calls.Load() != 3 {
		t.Fatal(result, err, calls.Load())
	}
}
func TestDiscoveryPlaylistEmbeddedFallbackAndTruncation(t *testing.T) {
	for _, test := range []struct {
		name             string
		ids              []string
		tracks           []string
		total, available int
		partial          bool
	}{
		{"embedded", nil, []string{discoverySong("1"), discoverySong("2")}, 5, 2, true},
		{"duplicate", []string{"1", "1", "2"}, []string{discoverySong("1"), discoverySong("2")}, 3, 2, true},
		{"empty", nil, nil, 0, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, calls := discoveryFixture(t, discoveryPlaylist(test.ids, test.tracks, test.total), "")
			result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0)
			if err != nil || result.AvailableCount != test.available || result.Partial != test.partial || calls.Load() != 1 {
				t.Fatal(result, err, calls.Load())
			}
		})
	}
	ids := make([]string, 1005)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	tracks := make([]string, 10)
	for i := range tracks {
		tracks[i] = discoverySong(fmt.Sprint(991 + i))
	}
	client, calls := discoveryFixture(t, discoveryPlaylist(ids, tracks, 1005), "")
	result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 990)
	if err != nil || !result.Partial || result.AvailableCount != 1000 || result.HasMore || result.NextOffset != 1000 || len(result.Tracks) != 10 || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
}
func TestDiscoveryPlaylistRejectsInvalidResponsesAndBounds(t *testing.T) {
	good := discoveryPlaylist([]string{"1"}, []string{discoverySong("1")}, 1)
	for _, body := range []string{
		`{}`, `{"code":200}`, `{"code":200,"playlist":null}`,
		strings.Replace(good, `"id":9007199254740993`, `"id":2`, 1),
		strings.Replace(good, `"trackCount":1`, `"trackCount":-1`, 1),
		strings.Replace(good, `"trackCount":1`, `"trackCount":1.5`, 1),
		strings.Replace(good, `"trackCount":1`, `"trackCount":null`, 1),
		strings.Replace(good, `"trackCount":1`, `"trackCount":0`, 1),
		strings.Replace(good, `"id":1`, `"id":1e3`, 1),
	} {
		client, _ := discoveryFixture(t, body, "")
		if _, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0); err == nil {
			t.Fatal("accepted", body)
		}
	}
	client, calls := discoveryFixture(t, good, "")
	for _, offset := range []int{-1, 1, 1000, 1020} {
		if _, err := client.GetPublicPlaylist(context.Background(), "1", offset); err == nil {
			t.Fatal(offset)
		}
	}
	if _, err := client.GetPublicPlaylist(context.Background(), "1e3", 0); err == nil {
		t.Fatal("unsafe ID")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input reached network")
	}
}
func TestDiscoveryBatchRejectsUnexpectedIDsAndProviderFailures(t *testing.T) {
	for _, test := range []struct{ batch, code string }{
		{`{"code":200,"songs":[` + discoverySong("2") + `]}`, "MISMATCHED_SONG_ID"},
		{`{"code":301}`, "LOGIN_REQUIRED"}, {`{"code":302}`, "LOGIN_REQUIRED"}, {`{"code":20001}`, "LOGIN_REQUIRED"}, {`{"code":429}`, "RATE_LIMITED"}, {`{`, "INVALID_PROVIDER_RESPONSE"},
	} {
		client, _ := discoveryFixture(t, discoveryPlaylist([]string{"1"}, nil, 1), test.batch)
		result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0)
		if err == nil || err.Error() != test.code || len(result.Tracks) != 0 {
			t.Fatal(result, err, test.code)
		}
	}
	client, _ := discoveryFixture(t, discoveryPlaylist([]string{"1", "2"}, nil, 2), `{"code":200,"songs":[`+discoverySong("1")+`,`+discoverySong("1")+`]}`)
	result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0)
	if err != nil || !result.Partial || len(result.Tracks) != 1 || result.NextOffset != 2 {
		t.Fatal(result, err)
	}
}
func TestDiscoveryToplistsLimitsCoversAndAnonymousTransport(t *testing.T) {
	items := make([]map[string]any, 105)
	for i := range items {
		items[i] = map[string]any{"id": json.Number(fmt.Sprint(9007199254740993 + int64(i))), "name": strings.Repeat("界", 200), "description": strings.Repeat("界", 1500), "trackCount": 10, "coverImgUrl": "http://p1.music.126.net/c.jpg"}
	}
	items[0]["coverImgUrl"] = "https://evil.test/a.jpg?token=secret"
	payload, _ := json.Marshal(map[string]any{"code": 200, "list": items})
	jar, _ := cookiejar.New(nil)
	calls := 0
	client := NewClientWithHTTP(&http.Client{Jar: jar, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != "https://music.163.com/api/toplist" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Real-IP") != "" {
			t.Fatal("anonymous boundary")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
	})}, "https://music.163.com")
	result, err := client.GetToplists(context.Background())
	if err != nil || !result.Partial || len(result.Items) != 100 || result.Items[0].PlaylistID != "9007199254740993" || result.Items[0].CoverRef != "" || calls != 1 {
		t.Fatal(result, err)
	}
	for _, item := range result.Items {
		if len(item.Name) > 512 || len(item.Description) > 4096 || !utf8.ValidString(item.Name) || !utf8.ValidString(item.Description) {
			t.Fatal("invalid text projection")
		}
	}
}
func TestDiscoveryToplistsRejectsMalformedOversizedAndRedirects(t *testing.T) {
	for _, test := range []struct{ body, code string }{
		{`{`, "INVALID_PROVIDER_RESPONSE"}, {`{"code":200}`, "INVALID_PROVIDER_RESPONSE"}, {`{"code":200,"list":[{"id":1,"name":"x","trackCount":-1}]}`, "INVALID_PROVIDER_RESPONSE"},
		{`{"code":200,"list":[{"id":1e3,"name":"x","trackCount":1}]}`, "INVALID_PROVIDER_RESPONSE"}, {`{"code":200,"list":[{"id":1,"name":"x"}]}`, "INVALID_PROVIDER_RESPONSE"},
		{`{"code":302}`, "LOGIN_REQUIRED"}, {`{"code":20001}`, "LOGIN_REQUIRED"}, {strings.Repeat(" ", 4*1024*1024+1), "PROVIDER_RESPONSE_TOO_LARGE"},
	} {
		client := NewClientWithHTTP(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}, nil
		})}, "https://music.163.com")
		if _, err := client.GetToplists(context.Background()); err == nil || err.Error() != test.code {
			t.Fatal(err, test.code)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://evil.test/", 302) }))
	defer server.Close()
	if _, err := NewClientWithHTTP(server.Client(), server.URL).GetToplists(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
}
func TestDiscoveryQueueAndSpacingCancellationDoNotBlockOtherCatalog(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/toplist" {
			close(entered)
			<-release
			io.WriteString(w, `{"code":200,"list":[]}`)
			return
		}
		io.WriteString(w, `{"code":200,"lrc":{"lyric":"ok"}}`)
	}))
	defer server.Close()
	client := NewClientWithHTTP(server.Client(), server.URL)
	first := make(chan error, 1)
	go func() { _, err := client.GetToplists(context.Background()); first <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.GetToplists(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if result, err := client.Lyrics(context.Background(), "1"); err != nil || result.Original != "ok" {
		t.Fatal(result, err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	// The second call expires during spacing and never reaches the handler.
	short, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if _, err := client.GetToplists(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestDiscoveryPlaylistSharedDeadlineStopsBatch(t *testing.T) {
	client, calls := discoveryFixture(t, discoveryPlaylist([]string{"1"}, nil, 1), `{"code":200,"songs":[]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.GetPublicPlaylist(ctx, "9007199254740993", 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("batch escaped common deadline", calls.Load())
	}
}

func TestDiscoveryPlaylistPartialInvalidMetadataAndResponseSize(t *testing.T) {
	for _, songs := range []string{`[]`, `[` + strings.Replace(discoverySong("1"), `"dt":100`, `"dt":86400001`, 1) + `]`, `[` + strings.Replace(discoverySong("1"), `"name":"song"`, `"name":""`, 1) + `]`} {
		client, calls := discoveryFixture(t, discoveryPlaylist([]string{"1"}, nil, 1), `{"code":200,"songs":`+songs+`}`)
		result, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0)
		if err != nil || !result.Partial || len(result.Tracks) != 0 || result.AvailableCount != 1 || result.NextOffset != 1 || calls.Load() != 2 {
			t.Fatal(result, err)
		}
	}
	client, _ := discoveryFixture(t, strings.Repeat(" ", 4*1024*1024+1), "")
	if _, err := client.GetPublicPlaylist(context.Background(), "9007199254740993", 0); err == nil || err.Error() != "PROVIDER_RESPONSE_TOO_LARGE" {
		t.Fatal(err)
	}
}

// A body may fail only after cancellation; the catalog must preserve the
// cancellation reason instead of converting it to a generic provider failure.
type discoveryCancelledBody struct{ cancel context.CancelFunc }

func (body discoveryCancelledBody) Read([]byte) (int, error) {
	body.cancel()
	return 0, context.Canceled
}
func (discoveryCancelledBody) Close() error { return nil }
func TestDiscoveryCancellationWhileReadingBody(t *testing.T) {
	for _, playlist := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		client := NewClientWithHTTP(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: discoveryCancelledBody{cancel: cancel}}, nil
		})}, "https://music.163.com")
		var err error
		if playlist {
			_, err = client.GetPublicPlaylist(ctx, "1", 0)
		} else {
			_, err = client.GetToplists(ctx)
		}
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal(playlist, err)
		}
	}
}

// HTTP rate limiting and the provider's business-level throttle must reach the UI
// as the same reason; unknown codes stay a plain provider failure.
func TestDiscoveryErrorCodes(t *testing.T) {
	for _, test := range []struct {
		code   int
		reason string
	}{
		{429, "RATE_LIMITED"},
		{-462, "RATE_LIMITED"},
		{301, "LOGIN_REQUIRED"},
		{20001, "LOGIN_REQUIRED"},
		{500, "PROVIDER_UNAVAILABLE"},
		{0, "PROVIDER_UNAVAILABLE"},
	} {
		if err := discoveryError(test.code); err == nil || err.Error() != test.reason {
			t.Fatalf("code %d: %v", test.code, err)
		}
	}
}
