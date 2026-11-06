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
	"unicode/utf8"
)

const discoverySpacing = 250 * time.Millisecond

// The discovery gate is independent from search, lyrics and playback. Its wait
// and every request in a playlist chain share the caller's finite deadline.
func (c *Client) enterDiscovery(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.discoveryGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *Client) paceDiscovery(ctx context.Context) error {
	if wait := time.Until(c.discoveryNext); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.discoveryNext = time.Now().Add(discoverySpacing)
	return nil
}

// Preserve cancellation even if it arrives while the shared POST helper reads
// the response body and reports its otherwise intentionally generic IO error.
func (c *Client) discoveryPost(ctx context.Context, path string, data any, out any) error {
	err := c.post(ctx, path, data, out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func discoveryError(code int) error {
	switch code {
	case 301, 302, 401, 403, 20001:
		return errors.New("LOGIN_REQUIRED")
	// -462 is the provider's business-level throttle; it arrives in the JSON body,
	// not as an HTTP status, so 429 alone would report it as a plain outage.
	case 429, -462:
		return errors.New("RATE_LIMITED")
	default:
		return errors.New("PROVIDER_UNAVAILABLE")
	}
}
func discoveryText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func discoveryCover(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	// Rewrite only the scheme instead of replacing the first occurrence anywhere in
	// the string; ValidCover then enforces https, host, path and absence of query.
	if parsed.Scheme == "http" {
		parsed.Scheme = "https"
	}
	if cover := parsed.String(); domain.ValidCover(cover) {
		return cover
	}
	return ""
}
func validDiscoveryCount(n *int) bool { return n != nil && *n >= 0 && *n <= 2147483647 }

func (c *Client) GetToplists(ctx context.Context) (domain.ToplistResult, error) {
	result := domain.ToplistResult{Items: []domain.Toplist{}}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := c.enterDiscovery(ctx); err != nil {
		return result, err
	}
	defer func() { <-c.discoveryGate }()
	if err := c.paceDiscovery(ctx); err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/toplist", nil)
	if err != nil {
		return result, errors.New("INVALID_PROVIDER_REQUEST")
	}
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("PROVIDER_UNAVAILABLE")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return result, discoveryError(resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("PROVIDER_UNAVAILABLE")
	}
	if len(raw) > 4*1024*1024 {
		return result, errors.New("PROVIDER_RESPONSE_TOO_LARGE")
	}
	var response struct {
		Code int `json:"code"`
		List []struct {
			ID          json.Number `json:"id"`
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Cover       string      `json:"coverImgUrl"`
			Count       *int        `json:"trackCount"`
		} `json:"list"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return result, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	if response.Code != 200 {
		return result, discoveryError(response.Code)
	}
	if response.List == nil {
		return result, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	seen := make(map[string]bool)
	for i, item := range response.List {
		if i >= 100 {
			result.Partial = true
			break
		}
		id := item.ID.String()
		if !domain.ValidID(id) || strings.TrimSpace(item.Name) == "" || !validDiscoveryCount(item.Count) {
			return domain.ToplistResult{}, errors.New("INVALID_PROVIDER_RESPONSE")
		}
		if seen[id] {
			result.Partial = true
			continue
		}
		seen[id] = true
		result.Items = append(result.Items, domain.Toplist{PlaylistID: id, Name: discoveryText(item.Name, 512), Description: discoveryText(item.Description, 4096), CoverRef: discoveryCover(item.Cover), TrackCount: *item.Count})
	}
	if ctx.Err() != nil {
		return domain.ToplistResult{}, ctx.Err()
	}
	return result, nil
}

func discoveryTrack(song remoteSong) (domain.TrackSnapshot, bool) {
	track := domain.TrackSnapshot{NeteaseSongID: song.ID.String(), Name: discoveryText(song.Name, 512), DurationMs: song.Duration, Artists: []domain.Artist{}, CoverRef: discoveryCover(song.Album.PicURL)}
	for _, artist := range song.Artists {
		id := artist.ID.String()
		if id == "0" {
			id = ""
		}
		track.Artists = append(track.Artists, domain.Artist{ID: id, Name: discoveryText(artist.Name, 512)})
	}
	if song.Album.ID != "" && song.Album.ID != "0" {
		track.Album = &domain.Album{ID: song.Album.ID.String(), Name: discoveryText(song.Album.Name, 512)}
	}
	return track, domain.ValidateTrack(track) == nil
}

func (c *Client) GetPublicPlaylist(ctx context.Context, id string, offset int) (domain.PublicPlaylistPage, error) {
	result := domain.PublicPlaylistPage{Tracks: []domain.TrackSnapshot{}}
	if err := domain.ValidatePublicPlaylistRequest(id, offset); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := c.enterDiscovery(ctx); err != nil {
		return result, err
	}
	defer func() { <-c.discoveryGate }()
	var response struct {
		Code     int `json:"code"`
		Playlist *struct {
			ID          json.Number `json:"id"`
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Cover       string      `json:"coverImgUrl"`
			Count       *int        `json:"trackCount"`
			IDs         []struct {
				ID json.Number `json:"id"`
			} `json:"trackIds"`
			Tracks []remoteSong `json:"tracks"`
		} `json:"playlist"`
	}
	if err := c.paceDiscovery(ctx); err != nil {
		return result, err
	}
	if err := c.discoveryPost(ctx, "/v3/playlist/detail", map[string]any{"id": id, "n": 30, "s": 0}, &response); err != nil {
		return result, err
	}
	if response.Code != 200 {
		return result, discoveryError(response.Code)
	}
	p := response.Playlist
	if p == nil {
		return result, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	if p.ID.String() != id {
		return result, errors.New("MISMATCHED_PLAYLIST_ID")
	}
	if !validDiscoveryCount(p.Count) || strings.TrimSpace(p.Name) == "" {
		return result, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	result.PlaylistID, result.Name, result.Description, result.CoverRef = id, discoveryText(p.Name, 512), discoveryText(p.Description, 4096), discoveryCover(p.Cover)
	result.Total, result.Offset = *p.Count, offset
	ids := make([]string, 0, 1000)
	seen := make(map[string]bool)
	if len(p.IDs) > 0 {
		for i, item := range p.IDs {
			if i >= 1000 {
				result.Partial = true
				break
			}
			songID := item.ID.String()
			if !domain.ValidID(songID) {
				return domain.PublicPlaylistPage{}, errors.New("INVALID_PROVIDER_RESPONSE")
			}
			if seen[songID] {
				result.Partial = true
				continue
			}
			seen[songID] = true
			ids = append(ids, songID)
		}
	} else {
		for i, song := range p.Tracks {
			if i >= 1000 {
				result.Partial = true
				break
			}
			track, valid := discoveryTrack(song)
			if !valid || seen[track.NeteaseSongID] {
				result.Partial = true
				continue
			}
			seen[track.NeteaseSongID] = true
			ids = append(ids, track.NeteaseSongID)
		}
	}
	if len(ids) > result.Total {
		return domain.PublicPlaylistPage{}, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	result.AvailableCount = len(ids)
	result.Partial = result.Partial || result.Total > len(ids)
	if offset >= len(ids) {
		result.NextOffset = len(ids)
		return result, ctx.Err()
	}
	end := min(offset+30, len(ids))
	pageIDs := ids[offset:end]
	result.NextOffset, result.HasMore = end, end < len(ids)
	wanted := make(map[string]bool, len(pageIDs))
	for _, songID := range pageIDs {
		wanted[songID] = true
	}
	tracks := make(map[string]domain.TrackSnapshot, len(pageIDs))
	for _, song := range p.Tracks {
		if !wanted[song.ID.String()] {
			continue
		}
		track, valid := discoveryTrack(song)
		if !valid {
			result.Partial = true
			continue
		}
		if _, exists := tracks[track.NeteaseSongID]; exists {
			result.Partial = true
			continue
		}
		tracks[track.NeteaseSongID] = track
	}
	missing := []string{}
	for _, songID := range pageIDs {
		if _, ok := tracks[songID]; !ok {
			missing = append(missing, songID)
		}
	}
	if len(missing) > 0 {
		var detail struct {
			Code  int          `json:"code"`
			Songs []remoteSong `json:"songs"`
		}
		// Validated decimal strings are inserted as JSON integer tokens, preserving
		// IDs above JavaScript's integer precision without any float conversion.
		batch := "[" + strings.Join(missing, ",") + "]"
		objects := make([]string, len(missing))
		expected := make(map[string]bool, len(missing))
		for i, songID := range missing {
			objects[i] = "{\"id\":" + songID + "}"
			expected[songID] = true
		}
		if err := c.paceDiscovery(ctx); err != nil {
			return domain.PublicPlaylistPage{}, err
		}
		if err := c.discoveryPost(ctx, "/v3/song/detail", map[string]any{"ids": batch, "c": "[" + strings.Join(objects, ",") + "]"}, &detail); err != nil {
			return domain.PublicPlaylistPage{}, err
		}
		if detail.Code != 200 {
			return domain.PublicPlaylistPage{}, discoveryError(detail.Code)
		}
		for _, song := range detail.Songs {
			songID := song.ID.String()
			if !expected[songID] {
				return domain.PublicPlaylistPage{}, errors.New("MISMATCHED_SONG_ID")
			}
			if _, exists := tracks[songID]; exists {
				result.Partial = true
				continue
			}
			track, valid := discoveryTrack(song)
			if !valid {
				result.Partial = true
				continue
			}
			tracks[songID] = track
		}
	}
	for _, songID := range pageIDs {
		if track, ok := tracks[songID]; ok {
			result.Tracks = append(result.Tracks, track)
		} else {
			result.Partial = true
		}
	}
	if ctx.Err() != nil {
		return domain.PublicPlaylistPage{}, ctx.Err()
	}
	return result, nil
}
