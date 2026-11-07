package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"strings"
	"time"
)

const (
	entityPageSize   = 30
	entityTrackLimit = 200
)

// The multi-type search reuses the one endpoint whose anonymous transport the
// song search already exercises; only the numeric type changes.
var entitySearchTypes = map[string]int{
	domain.EntityAlbum:    10,
	domain.EntityArtist:   100,
	domain.EntityPlaylist: 1000,
}

func entityCount(n *int) int {
	if n == nil || !validDiscoveryCount(n) {
		return 0
	}
	return *n
}

func (c *Client) SearchEntities(ctx context.Context, query, kind string, page int) (domain.EntitySearchResult, error) {
	result := domain.EntitySearchResult{Kind: kind, Items: []domain.EntitySummary{}}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 512 || page < 0 || page > 1000 || !domain.ValidEntitySearchKind(kind) {
		return result, errors.New("INVALID_ENTITY_REQUEST")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := c.enterDiscovery(ctx); err != nil {
		return result, err
	}
	defer func() { <-c.discoveryGate }()
	if err := c.paceDiscovery(ctx); err != nil {
		return result, err
	}
	var r struct {
		Code   int `json:"code"`
		Result struct {
			Albums []struct {
				ID     json.Number `json:"id"`
				Name   string      `json:"name"`
				Artist *struct {
					Name string `json:"name"`
				} `json:"artist"`
				PicURL string `json:"picUrl"`
				Size   *int   `json:"size"`
			} `json:"albums"`
			Artists []struct {
				ID        json.Number `json:"id"`
				Name      string      `json:"name"`
				PicURL    string      `json:"picUrl"`
				ImgURL    string      `json:"img1v1Url"`
				AlbumSize *int        `json:"albumSize"`
			} `json:"artists"`
			Playlists []struct {
				ID      json.Number `json:"id"`
				Name    string      `json:"name"`
				Creator *struct {
					Nickname string `json:"nickname"`
				} `json:"creator"`
				CoverImgURL string `json:"coverImgUrl"`
				TrackCount  *int   `json:"trackCount"`
			} `json:"playlists"`
			AlbumCount    int `json:"albumCount"`
			ArtistCount   int `json:"artistCount"`
			PlaylistCount int `json:"playlistCount"`
		} `json:"result"`
	}
	if err := c.discoveryPost(ctx, "/cloudsearch/pc", map[string]any{
		"s": query, "type": entitySearchTypes[kind], "limit": entityPageSize, "offset": page * entityPageSize, "total": true,
	}, &r); err != nil {
		return result, err
	}
	if r.Code != 200 {
		return result, discoveryError(r.Code)
	}

	// Project each kind through the same bounded, trust-checked shape. Invalid rows
	// are skipped and reported as partial instead of dropping the whole page.
	add := func(id, name, subtitle, cover string, count int) {
		if len(result.Items) >= entityPageSize {
			result.Partial = true
			return
		}
		if !domain.ValidID(id) || strings.TrimSpace(name) == "" {
			result.Partial = true
			return
		}
		result.Items = append(result.Items, domain.EntitySummary{
			ID: id, Name: discoveryText(name, 512), Subtitle: discoveryText(subtitle, 512),
			CoverRef: discoveryCover(cover), TrackCount: count,
		})
	}
	switch kind {
	case domain.EntityAlbum:
		for _, a := range r.Result.Albums {
			artist := ""
			if a.Artist != nil {
				artist = a.Artist.Name
			}
			add(a.ID.String(), a.Name, artist, a.PicURL, entityCount(a.Size))
		}
		result.Total = r.Result.AlbumCount
	case domain.EntityArtist:
		for _, a := range r.Result.Artists {
			cover := a.PicURL
			if cover == "" {
				cover = a.ImgURL
			}
			add(a.ID.String(), a.Name, "", cover, entityCount(a.AlbumSize))
		}
		result.Total = r.Result.ArtistCount
	case domain.EntityPlaylist:
		for _, p := range r.Result.Playlists {
			creator := ""
			if p.Creator != nil {
				creator = p.Creator.Nickname
			}
			add(p.ID.String(), p.Name, creator, p.CoverImgURL, entityCount(p.TrackCount))
		}
		result.Total = r.Result.PlaylistCount
	}
	if result.Total <= 0 {
		result.Total = len(result.Items)
	}
	result.HasMore = (page+1)*entityPageSize < result.Total
	if ctx.Err() != nil {
		return domain.EntitySearchResult{}, ctx.Err()
	}
	return result, nil
}

func (c *Client) GetEntityDetail(ctx context.Context, kind, id string) (domain.EntityDetail, error) {
	result := domain.EntityDetail{Kind: kind, ID: id, Tracks: []domain.TrackSnapshot{}}
	if !domain.ValidEntityDetailKind(kind) || !domain.ValidID(id) {
		return result, errors.New("INVALID_ENTITY_REQUEST")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := c.enterDiscovery(ctx); err != nil {
		return result, err
	}
	defer func() { <-c.discoveryGate }()
	if err := c.paceDiscovery(ctx); err != nil {
		return result, err
	}
	path := "/v1/album/" + id
	data := map[string]any{}
	if kind == domain.EntityArtist {
		path = "/v1/artist/" + id
		data["id"] = id
	}
	var r struct {
		Code  int `json:"code"`
		Album *struct {
			ID          json.Number `json:"id"`
			Name        string      `json:"name"`
			PicURL      string      `json:"picUrl"`
			Description string      `json:"description"`
			Artist      *struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"album"`
		Artist *struct {
			ID        json.Number `json:"id"`
			Name      string      `json:"name"`
			PicURL    string      `json:"picUrl"`
			BriefDesc string      `json:"briefDesc"`
		} `json:"artist"`
		Songs    []remoteSong `json:"songs"`
		HotSongs []remoteSong `json:"hotSongs"`
	}
	if err := c.discoveryPost(ctx, path, data, &r); err != nil {
		return result, err
	}
	if r.Code != 200 {
		return result, discoveryError(r.Code)
	}
	raw := r.Songs
	switch kind {
	case domain.EntityAlbum:
		if r.Album == nil || r.Album.ID.String() != id {
			return result, errors.New("MISMATCHED_ENTITY_ID")
		}
		result.Name = discoveryText(r.Album.Name, 512)
		result.Description = discoveryText(r.Album.Description, 4096)
		result.CoverRef = discoveryCover(r.Album.PicURL)
		if r.Album.Artist != nil {
			result.Subtitle = discoveryText(r.Album.Artist.Name, 512)
		}
	case domain.EntityArtist:
		if r.Artist == nil || r.Artist.ID.String() != id {
			return result, errors.New("MISMATCHED_ENTITY_ID")
		}
		result.Name = discoveryText(r.Artist.Name, 512)
		result.Description = discoveryText(r.Artist.BriefDesc, 4096)
		result.CoverRef = discoveryCover(r.Artist.PicURL)
		raw = r.HotSongs
	}
	if strings.TrimSpace(result.Name) == "" {
		return result, errors.New("INVALID_PROVIDER_RESPONSE")
	}
	seen := make(map[string]bool)
	for _, song := range raw {
		if len(result.Tracks) >= entityTrackLimit {
			result.Partial = true
			break
		}
		// An album page must not surface tracks the provider attributes to another
		// album. An empty al.id is unknown rather than a mismatch, so it is kept.
		if kind == domain.EntityAlbum {
			if albumID := song.Album.ID.String(); albumID != "" && albumID != id {
				result.Partial = true
				continue
			}
		}
		track, valid := discoveryTrack(song)
		if !valid || seen[track.NeteaseSongID] {
			result.Partial = true
			continue
		}
		seen[track.NeteaseSongID] = true
		result.Tracks = append(result.Tracks, track)
	}
	result.Total = len(result.Tracks)
	if ctx.Err() != nil {
		return domain.EntityDetail{}, ctx.Err()
	}
	if err := domain.ValidateEntityDetail(result); err != nil {
		return domain.EntityDetail{}, err
	}
	return result, nil
}
