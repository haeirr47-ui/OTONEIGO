package domain

import "errors"

// Discovery is anonymous, bounded metadata; it never contains playback URLs.
type Toplist struct {
	PlaylistID  string `json:"playlistId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CoverRef    string `json:"coverRef"`
	TrackCount  int    `json:"trackCount"`
}
type ToplistResult struct {
	Items   []Toplist `json:"items"`
	Partial bool      `json:"partial"`
}
type PublicPlaylistPage struct {
	PlaylistID     string          `json:"playlistId"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	CoverRef       string          `json:"coverRef"`
	Tracks         []TrackSnapshot `json:"tracks"`
	Total          int             `json:"total"`
	AvailableCount int             `json:"availableCount"`
	Offset         int             `json:"offset"`
	NextOffset     int             `json:"nextOffset"`
	HasMore        bool            `json:"hasMore"`
	Partial        bool            `json:"partial"`
}

func ValidatePublicPlaylistRequest(id string, offset int) error {
	if !ValidID(id) {
		return errors.New("INVALID_PLAYLIST_ID")
	}
	if offset < 0 || offset > 990 || offset%30 != 0 {
		return errors.New("INVALID_PLAYLIST_OFFSET")
	}
	return nil
}
