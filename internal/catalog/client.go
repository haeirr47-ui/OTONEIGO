package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	http *http.Client
	base string
}

func NewClient() *Client {
	return NewClientWithHTTP(&http.Client{Timeout: 12 * time.Second}, "https://music.163.com")
}

// NewClientWithHTTP permits isolated HTTP fixture tests. Production uses NewClient.
func NewClientWithHTTP(h *http.Client, base string) *Client {
	copyClient := *h
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return errors.New("PROVIDER_REDIRECT_REJECTED") }
	return &Client{http: &copyClient, base: strings.TrimRight(base, "/")}
}
func (c *Client) post(ctx context.Context, path string, data any, out any) error {
	return c.postStrategy(ctx, path, data, out, false)
}

// RegionHintVersion identifies the bounded, best-effort public protocol hint.
const RegionHintVersion = "2026-10-31-v1"
const regionHint = "211.161.244.70"

func (c *Client) postStrategy(ctx context.Context, path string, data any, out any, compatible bool) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	form, err := weapi(data)
	if err != nil {
		return errors.New("REQUEST_ENCODING_FAILED")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/weapi"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("INVALID_PROVIDER_REQUEST")
	}
	if compatible {
		if req.Method != http.MethodPost || req.URL.Scheme != "https" || req.URL.Host != "music.163.com" || req.URL.Path != "/weapi/song/enhance/player/url/v1" || req.URL.RawQuery != "" || req.URL.User != nil {
			return errors.New("COMPATIBILITY_ENDPOINT_REJECTED")
		}
		req.Header.Set("X-Real-IP", regionHint)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("PROVIDER_UNAVAILABLE")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return errors.New("LOGIN_REQUIRED")
	}
	if resp.StatusCode == 429 {
		return errors.New("RATE_LIMITED")
	}
	if resp.StatusCode != 200 {
		return errors.New("PROVIDER_UNAVAILABLE")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil {
		return errors.New("PROVIDER_UNAVAILABLE")
	}
	if len(raw) > 4*1024*1024 {
		return errors.New("PROVIDER_RESPONSE_TOO_LARGE")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return errors.New("INVALID_PROVIDER_RESPONSE")
	}
	return ctx.Err()
}

type remoteArtist struct {
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
}
type remoteAlbum struct {
	ID     json.Number `json:"id"`
	Name   string      `json:"name"`
	PicURL string      `json:"picUrl"`
}
type remoteSong struct {
	ID       json.Number    `json:"id"`
	Name     string         `json:"name"`
	Artists  []remoteArtist `json:"ar"`
	Album    remoteAlbum    `json:"al"`
	Duration int64          `json:"dt"`
}

func (c *Client) Search(ctx context.Context, query string, page int) (domain.SearchResult, error) {
	result := domain.SearchResult{Tracks: []domain.TrackSnapshot{}}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 512 || page < 0 || page > 1000 {
		return result, errors.New("INVALID_SEARCH")
	}
	var r struct {
		Code   int `json:"code"`
		Result struct {
			Songs     []remoteSong `json:"songs"`
			SongCount int          `json:"songCount"`
		} `json:"result"`
	}
	if err := c.post(ctx, "/cloudsearch/pc", map[string]any{"s": query, "type": 1, "limit": 30, "offset": page * 30, "total": true}, &r); err != nil {
		return result, err
	}
	if r.Code != 200 {
		return result, providerError(r.Code)
	}
	for _, s := range r.Result.Songs {
		if !domain.ValidID(s.ID.String()) || s.Name == "" || s.Duration < 0 {
			return result, errors.New("INVALID_PROVIDER_RESPONSE")
		}
		t := domain.TrackSnapshot{NeteaseSongID: s.ID.String(), Name: s.Name, Artists: []domain.Artist{}, DurationMs: s.Duration}
		for _, a := range s.Artists {
			if a.ID != "" && a.ID != "0" && !domain.ValidID(a.ID.String()) {
				return result, errors.New("INVALID_PROVIDER_RESPONSE")
			}
			artistID := a.ID.String()
			if artistID == "0" {
				artistID = ""
			}
			t.Artists = append(t.Artists, domain.Artist{ID: artistID, Name: a.Name})
		}
		if s.Album.ID != "" && domain.ValidID(s.Album.ID.String()) {
			t.Album = &domain.Album{ID: s.Album.ID.String(), Name: s.Album.Name}
		}
		cover := strings.Replace(s.Album.PicURL, "http://", "https://", 1)
		if domain.ValidCover(cover) {
			t.CoverRef = cover
		}
		result.Tracks = append(result.Tracks, t)
	}
	result.Total = r.Result.SongCount
	result.HasMore = (page*30 + len(result.Tracks)) < result.Total
	return result, nil
}

// GetSong fetches canonical metadata through the ordinary catalog path. No
// compatibility hint, session credentials or media URLs are added to snapshots.
func (c *Client) GetSong(ctx context.Context, id string) (domain.TrackSnapshot, error) {
	var result domain.TrackSnapshot
	if !domain.ValidID(id) {
		return result, errors.New("INVALID_SONG_ID")
	}
	var response struct {
		Code  int          `json:"code"`
		Songs []remoteSong `json:"songs"`
	}
	data := map[string]any{"ids": "[" + id + "]", "c": "[{\"id\":" + id + "}]"}
	if err := c.post(ctx, "/v3/song/detail", data, &response); err != nil {
		return result, err
	}
	if response.Code != 200 {
		return result, providerError(response.Code)
	}
	if len(response.Songs) != 1 || response.Songs[0].ID.String() != id {
		return result, errors.New("MISMATCHED_SONG_ID")
	}
	song := response.Songs[0]
	result = domain.TrackSnapshot{NeteaseSongID: id, Name: song.Name, Artists: []domain.Artist{}, DurationMs: song.Duration}
	for _, artist := range song.Artists {
		artistID := artist.ID.String()
		if artistID == "0" {
			artistID = ""
		}
		result.Artists = append(result.Artists, domain.Artist{ID: artistID, Name: artist.Name})
	}
	if song.Album.ID != "" && song.Album.ID != "0" {
		result.Album = &domain.Album{ID: song.Album.ID.String(), Name: song.Album.Name}
	}
	cover := strings.Replace(song.Album.PicURL, "http://", "https://", 1)
	if domain.ValidCover(cover) {
		result.CoverRef = cover
	}
	if domain.ValidateTrack(result) != nil {
		return domain.TrackSnapshot{}, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	return result, nil
}

func providerError(code int) error {
	if code == 301 || code == 401 || code == 403 {
		return errors.New("LOGIN_REQUIRED")
	}
	if code == 429 {
		return errors.New("RATE_LIMITED")
	}
	return errors.New("PROVIDER_UNAVAILABLE")
}
func (c *Client) Lyrics(ctx context.Context, id string) (domain.LyricData, error) {
	var result domain.LyricData
	if !domain.ValidID(id) {
		return result, errors.New("INVALID_SONG_ID")
	}
	var r struct {
		Code int `json:"code"`
		LRC  struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		TLRC struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
	}
	if err := c.post(ctx, "/song/lyric", map[string]any{"id": id, "lv": -1, "kv": -1, "tv": -1}, &r); err != nil {
		return result, err
	}
	if r.Code != 200 {
		return result, providerError(r.Code)
	}
	result.Original = r.LRC.Lyric
	result.Translated = r.TLRC.Lyric
	return result, nil
}
func trustedAudioURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Port() != "" || u.Fragment != "" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".music.126.net") || u.Path == "" {
		return "", errors.New("UNTRUSTED_MEDIA_URL")
	}
	u.Scheme = "https"
	return u.String(), nil
}
func (c *Client) Resolve(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	return c.resolve(ctx, id, quality, false)
}

func (c *Client) ResolveCompatible(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	return c.resolve(ctx, id, quality, true)
}

func (c *Client) resolve(ctx context.Context, id, quality string, compatible bool) (domain.ResolvedStream, error) {
	result := domain.ResolvedStream{SongID: id, Provider: "netease-direct", RequestedQuality: quality, Status: "unknown", AccessStrategy: "normal"}
	if compatible {
		result.AccessStrategy = "region-header"
	}
	if !domain.ValidID(id) {
		return result, errors.New("INVALID_SONG_ID")
	}
	switch quality {
	case "standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster":
	default:
		return result, errors.New("INVALID_QUALITY")
	}
	var r struct {
		Code int `json:"code"`
		Data []struct {
			ID            json.Number     `json:"id"`
			URL           string          `json:"url"`
			Code          int             `json:"code"`
			Level         string          `json:"level"`
			Type          string          `json:"type"`
			Time          int64           `json:"time"`
			Expi          int64           `json:"expi"`
			Fee           int             `json:"fee"`
			BR            int64           `json:"br"`
			Size          int64           `json:"size"`
			FreeTrialInfo json.RawMessage `json:"freeTrialInfo"`
		} `json:"data"`
	}
	encodeType := "mp3"
	switch quality {
	case "lossless", "hires", "jyeffect", "sky", "jymaster":
		encodeType = "flac"
	}
	request := map[string]any{"ids": "[" + id + "]", "level": quality, "encodeType": encodeType}
	if quality == "sky" {
		request["immerseType"] = "c51"
	}
	if err := c.postStrategy(ctx, "/song/enhance/player/url/v1", request, &r, compatible); err != nil {
		return result, err
	}
	if r.Code != 200 {
		return result, providerError(r.Code)
	}
	if len(r.Data) != 1 || r.Data[0].ID.String() != id {
		return result, errors.New("MISMATCHED_SONG_ID")
	}
	d := r.Data[0]
	trial := strings.TrimSpace(string(d.FreeTrialInfo))
	if trial != "" && trial != "null" {
		result.Status = "trial"
		result.ErrorCode = "TRIAL_ONLY"
	}
	if d.Code == 401 || d.Code == 403 || d.Code == -110 {
		result.Status = "unauthorized"
		result.ErrorCode = "UNAUTHORIZED"
		return result, nil
	}
	if d.Code == 301 || d.Code == 429 {
		return result, providerError(d.Code)
	}
	if d.URL == "" {
		if result.Status != "trial" {
			result.Status = "unavailable"
			result.ErrorCode = "EMPTY_URL"
		}
		return result, nil
	}
	if d.Code != 200 {
		return result, providerError(d.Code)
	}
	media, err := trustedAudioURL(d.URL)
	if err != nil {
		return result, err
	}
	result.URL = media
	if d.Size > 0 && d.Size <= 8*1024*1024*1024 {
		result.ContentLengthBytes = d.Size
	}
	if d.BR > 0 {
		result.ActualBitrate = d.BR
	}
	result.ActualQuality = d.Level
	result.ActualFormat = d.Type
	result.DurationMs = d.Time
	// Full means the provider explicitly returned no trial metadata, not that
	// native playback or the recording's complete duration has been verified.
	trial = strings.TrimSpace(string(d.FreeTrialInfo))
	if trial != "" && trial != "null" {
		result.Status = "trial"
	} else if trial == "null" && d.Time > 0 {
		result.Status = "full"
	}
	if d.Expi > 0 && d.Expi <= 86400 {
		result.ExpiresAt = time.Now().Add(time.Duration(d.Expi) * time.Second).UnixMilli()
	}
	return result, nil
}
