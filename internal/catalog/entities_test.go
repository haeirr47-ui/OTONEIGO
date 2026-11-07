package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func entityResponse(t *testing.T, body string, check func(*http.Request)) *Client {
	t.Helper()
	return NewClientWithHTTP(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("Cookie") != "" || r.Header.Get("X-Real-IP") != "" {
			t.Fatalf("anonymous boundary: %s %s", r.Method, r.URL.Path)
		}
		if check != nil {
			check(r)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, "https://music.163.com")
}

func entitySong(id int, albumID string) string {
	album := `"al":{"name":"专辑","picUrl":"https://p1.music.126.net/a.jpg"}`
	if albumID != "" {
		album = fmt.Sprintf(`"al":{"id":%s,"name":"专辑","picUrl":"https://p1.music.126.net/a.jpg"}`, albumID)
	}
	return fmt.Sprintf(`{"id":%d,"name":"歌曲 %d","ar":[{"id":222,"name":"歌手"}],%s,"dt":120000}`, id, id, album)
}

func TestEntitySearchProjectionAndAnonymousTransport(t *testing.T) {
	cases := []struct {
		kind, body, path string
		wantID           string
		wantCount        int
		wantTotal        int
		wantPartial      bool
		wantHasMore      bool
	}{
		{
			kind: "album", path: "/weapi/cloudsearch/pc", wantID: "9007199254740993", wantCount: 12, wantTotal: 45, wantPartial: true, wantHasMore: true,
			body: `{"code":200,"result":{"albums":[` +
				`{"id":9007199254740993,"name":"专辑一","artist":{"name":"歌手甲"},"picUrl":"http://p1.music.126.net/a.jpg","size":12},` +
				`{"id":2,"name":"","artist":null,"picUrl":"https://evil.test/x.jpg","size":3},` +
				`{"id":0,"name":"坏ID","picUrl":"https://p2.music.126.net/b.jpg"}],"albumCount":45}}`,
		},
		{
			kind: "artist", path: "/weapi/cloudsearch/pc", wantID: "123", wantCount: 7, wantTotal: 1,
			body: `{"code":200,"result":{"artists":[{"id":123,"name":"歌手","img1v1Url":"https://p1.music.126.net/c.jpg","albumSize":7}],"artistCount":1}}`,
		},
		{
			kind: "playlist", path: "/weapi/cloudsearch/pc", wantID: "456", wantCount: 30, wantTotal: 1,
			body: `{"code":200,"result":{"playlists":[{"id":456,"name":"歌单","creator":{"nickname":"用户"},"coverImgUrl":"https://p1.music.126.net/d.jpg","trackCount":30}],"playlistCount":1}}`,
		},
	}
	for _, tc := range cases {
		client := entityResponse(t, tc.body, func(r *http.Request) {
			if r.URL.Path != tc.path {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		})
		result, err := client.SearchEntities(context.Background(), "查询", tc.kind, 0)
		if err != nil {
			t.Fatal(tc.kind, err)
		}
		if result.Kind != tc.kind || len(result.Items) != 1 || result.Items[0].ID != tc.wantID ||
			result.Items[0].TrackCount != tc.wantCount || result.Total != tc.wantTotal ||
			result.Partial != tc.wantPartial || result.HasMore != tc.wantHasMore {
			t.Fatalf("%s: %+v", tc.kind, result)
		}
		if strings.HasPrefix(result.Items[0].CoverRef, "https://evil.test") || (result.Items[0].CoverRef != "" && !strings.HasPrefix(result.Items[0].CoverRef, "https://p1.music.126.net/")) {
			t.Fatalf("%s: untrusted cover %q", tc.kind, result.Items[0].CoverRef)
		}
	}
}

func TestEntitySearchBoundsErrorsAndPageLimit(t *testing.T) {
	client := entityResponse(t, `{"code":200,"result":{"albums":[],"albumCount":0}}`, nil)
	for _, tc := range []struct {
		query, kind string
		page        int
	}{
		{"", "album", 0}, {"a", "song", 0}, {"a", "", 0}, {"a", "album", -1}, {"a", "album", 1001}, {strings.Repeat("x", 513), "album", 0},
	} {
		if _, err := client.SearchEntities(context.Background(), tc.query, tc.kind, tc.page); err == nil || err.Error() != "INVALID_ENTITY_REQUEST" {
			t.Fatalf("%+v: %v", tc, err)
		}
	}

	rows := make([]string, 0, 32)
	for i := 1; i <= 32; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":%d,"name":"专辑 %d","picUrl":"https://p1.music.126.net/a.jpg","size":1}`, i, i))
	}
	full := entityResponse(t, `{"code":200,"result":{"albums":[`+strings.Join(rows, ",")+`],"albumCount":32}}`, nil)
	result, err := full.SearchEntities(context.Background(), "查询", "album", 0)
	if err != nil || len(result.Items) != entityPageSize || !result.Partial || !result.HasMore {
		t.Fatal(result, err)
	}

	codes := map[int]string{301: "LOGIN_REQUIRED", -462: "RATE_LIMITED", 500: "PROVIDER_UNAVAILABLE"}
	for code, reason := range codes {
		failing := entityResponse(t, fmt.Sprintf(`{"code":%d}`, code), nil)
		if _, err := failing.SearchEntities(context.Background(), "查询", "album", 0); err == nil || err.Error() != reason {
			t.Fatalf("code %d: %v", code, err)
		}
	}
}

func TestEntityDetailAlbumAndArtist(t *testing.T) {
	const rootID = "9007199254740993"
	albumSongs := []string{entitySong(111, rootID), entitySong(222, rootID), `{"id":0,"name":"坏ID","dt":1000}`}
	artistSongs := []string{entitySong(111, "333"), entitySong(222, "333"), `{"id":0,"name":"坏ID","dt":1000}`}
	for _, tc := range []struct {
		kind, path, body string
	}{
		{
			"album", "/weapi/v1/album/" + rootID,
			`{"code":200,"album":{"id":9007199254740993,"name":"专辑","picUrl":"http://p1.music.126.net/a.jpg","description":"简介","artist":{"name":"歌手"}},"songs":[` + strings.Join(albumSongs, ",") + `]}`,
		},
		{
			"artist", "/weapi/v1/artist/" + rootID,
			`{"code":200,"artist":{"id":9007199254740993,"name":"歌手","picUrl":"https://p1.music.126.net/c.jpg","briefDesc":"简介"},"hotSongs":[` + strings.Join(artistSongs, ",") + `]}`,
		},
	} {
		client := entityResponse(t, tc.body, func(r *http.Request) {
			if r.URL.Path != tc.path {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		})
		result, err := client.GetEntityDetail(context.Background(), tc.kind, "9007199254740993")
		if err != nil {
			t.Fatal(tc.kind, err)
		}
		if result.Kind != tc.kind || result.Name == "" || result.Description != "简介" || len(result.Tracks) != 2 ||
			result.Total != 2 || !result.Partial || !strings.HasPrefix(result.CoverRef, "https://p1.music.126.net/") {
			t.Fatalf("%s: %+v", tc.kind, result)
		}
	}

	mismatch := entityResponse(t, `{"code":200,"album":{"id":7,"name":"别的专辑"},"songs":[]}`, nil)
	if _, err := mismatch.GetEntityDetail(context.Background(), "album", "9007199254740993"); err == nil || err.Error() != "MISMATCHED_ENTITY_ID" {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kind, id string }{{"playlist", "1"}, {"album", "0"}, {"", "1"}} {
		if _, err := mismatch.GetEntityDetail(context.Background(), tc.kind, tc.id); err == nil || err.Error() != "INVALID_ENTITY_REQUEST" {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

func TestEntityDetailBoundsAndLoginRequired(t *testing.T) {
	rows := make([]string, 0, entityTrackLimit+2)
	for i := 1; i <= entityTrackLimit+2; i++ {
		rows = append(rows, entitySong(i, "1"))
	}
	client := entityResponse(t, `{"code":200,"album":{"id":"1","name":"专辑"},"songs":[`+strings.Join(rows, ",")+`]}`, nil)
	result, err := client.GetEntityDetail(context.Background(), "album", "1")
	if err != nil || len(result.Tracks) != entityTrackLimit || !result.Partial {
		t.Fatal(len(result.Tracks), err)
	}
	login := entityResponse(t, `{"code":301}`, nil)
	if _, err := login.GetEntityDetail(context.Background(), "artist", "1"); err == nil || err.Error() != "LOGIN_REQUIRED" {
		t.Fatal(err)
	}
}

func TestEntityAlbumDetailRejectsForeignTracks(t *testing.T) {
	body := `{"code":200,"album":{"id":"1","name":"专辑"},"songs":[` +
		entitySong(111, "2") + `,` + entitySong(222, "1") + `,` + entitySong(333, "") + `]}`
	client := entityResponse(t, body, nil)
	result, err := client.GetEntityDetail(context.Background(), "album", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tracks) != 2 || result.Tracks[0].NeteaseSongID != "222" || result.Tracks[1].NeteaseSongID != "333" || !result.Partial {
		t.Fatalf("%+v", result)
	}
}
