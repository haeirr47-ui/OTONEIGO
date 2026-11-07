package domain

import (
	"errors"
	"strings"
)

// Entity kinds are independent, non-persisted view models. The song kind stays on
// the existing SearchResult path; only these three use the multi-type search.
const (
	EntityAlbum    = "album"
	EntityArtist   = "artist"
	EntityPlaylist = "playlist"
)

var errInvalidEntity = errors.New("INVALID_ENTITY")

func ValidEntitySearchKind(kind string) bool {
	return kind == EntityAlbum || kind == EntityArtist || kind == EntityPlaylist
}
func ValidEntityDetailKind(kind string) bool {
	return kind == EntityAlbum || kind == EntityArtist
}

// EntitySummary is one row in a multi-type search result. TrackCount is zero when
// the provider does not report it; it never implies playback rights.
type EntitySummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Subtitle   string `json:"subtitle,omitempty"`
	CoverRef   string `json:"coverRef,omitempty"`
	TrackCount int    `json:"trackCount,omitempty"`
}

type EntitySearchResult struct {
	Kind    string          `json:"kind"`
	Items   []EntitySummary `json:"items"`
	Total   int             `json:"total"`
	HasMore bool            `json:"hasMore"`
	Partial bool            `json:"partial"`
}

// EntityDetail covers album and artist pages. Tracks are bounded by the catalog;
// Total is the number of tracks actually browsable in this response.
type EntityDetail struct {
	Kind        string          `json:"kind"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Subtitle    string          `json:"subtitle,omitempty"`
	Description string          `json:"description,omitempty"`
	CoverRef    string          `json:"coverRef,omitempty"`
	Tracks      []TrackSnapshot `json:"tracks"`
	Total       int             `json:"total"`
	Partial     bool            `json:"partial"`
}

func ValidateEntityDetail(d EntityDetail) error {
	if !ValidEntityDetailKind(d.Kind) || !ValidID(d.ID) || strings.TrimSpace(d.Name) == "" || len(d.Name) > 512 ||
		len(d.Subtitle) > 512 || len(d.Description) > 4096 || !ValidCover(d.CoverRef) || len(d.Tracks) > 200 || d.Total < len(d.Tracks) {
		return errInvalidEntity
	}
	for _, track := range d.Tracks {
		if ValidateTrack(track) != nil {
			return errInvalidEntity
		}
	}
	return nil
}
