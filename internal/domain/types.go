package domain

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

type Artist struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}
type Album struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}
type TrackSnapshot struct {
	NeteaseSongID string   `json:"neteaseSongId"`
	Name          string   `json:"name"`
	Artists       []Artist `json:"artists"`
	Album         *Album   `json:"album,omitempty"`
	DurationMs    int64    `json:"durationMs"`
	CoverRef      string   `json:"coverRef,omitempty"`
}
type QueueEntry struct {
	QueueEntryID string        `json:"queueEntryId"`
	Track        TrackSnapshot `json:"track"`
}
type QueueSnapshot struct {
	Entries        []QueueEntry `json:"entries"`
	CurrentEntryID *string      `json:"currentEntryId"`
	CurrentIndex   int          `json:"currentIndex"`
	PlayMode       string       `json:"playMode"`
	PositionMs     int64        `json:"positionMs"`
	Revision       int64        `json:"revision"`
}
type SearchResult struct {
	Tracks  []TrackSnapshot `json:"tracks"`
	Total   int             `json:"total"`
	HasMore bool            `json:"hasMore"`
}
type LyricData struct {
	Original   string `json:"original"`
	Translated string `json:"translated"`
}
type ResolvedStream struct {
	CacheHit             bool   `json:"cacheHit,omitempty"`
	CacheToken           string `json:"cacheToken,omitempty"`
	CacheEntryID         string `json:"-"`
	ContentLengthBytes   int64  `json:"contentLengthBytes,omitempty"`
	ResolutionDeadlineAt int64  `json:"resolutionDeadlineAt,omitempty"`
	AccessStrategy       string `json:"accessStrategy"`
	ErrorCode            string `json:"errorCode,omitempty"`
	RegionSuspected      bool   `json:"regionSuspected,omitempty"`
	ActualBitrate        int64  `json:"actualBitrate,omitempty"`
	SongID               string `json:"songId"`
	Provider             string `json:"provider"`
	URL                  string `json:"url"`
	RequestedQuality     string `json:"requestedQuality"`
	ActualQuality        string `json:"actualQuality,omitempty"`
	ActualFormat         string `json:"actualFormat,omitempty"`
	Status               string `json:"status"`
	ExpiresAt            int64  `json:"expiresAt,omitempty"`
	DurationMs           int64  `json:"durationMs,omitempty"`
}

var digits = regexp.MustCompile(`^[1-9][0-9]{0,29}$`)
var entryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidID(id string) bool { return digits.MatchString(id) }
func ValidCover(ref string) bool {
	if ref == "" {
		return true
	}
	if len(ref) > 4096 {
		return false
	}
	u, e := url.Parse(ref)
	if e != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
	default:
		return false
	}
	return e == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.RawQuery == "" && u.Fragment == "" && strings.HasSuffix(strings.ToLower(u.Hostname()), ".music.126.net") && u.Path != ""
}
func ValidateTrack(t TrackSnapshot) error {
	invalid := errors.New("INVALID_TRACK_SNAPSHOT")
	if !ValidID(t.NeteaseSongID) || len(strings.TrimSpace(t.Name)) == 0 || len(t.Name) > 1024 || len(t.Artists) > 100 || t.DurationMs < 0 || t.DurationMs > 86400000 || !ValidCover(t.CoverRef) {
		return invalid
	}
	for _, a := range t.Artists {
		if (a.ID != "" && !ValidID(a.ID)) || len(a.Name) > 1024 {
			return invalid
		}
	}
	if t.Album != nil && ((t.Album.ID != "" && !ValidID(t.Album.ID)) || len(t.Album.Name) > 1024) {
		return invalid
	}
	return nil
}
func ValidateQueue(q QueueSnapshot) error {
	invalid := errors.New("INVALID_QUEUE_SNAPSHOT")
	if q.Revision < 1 || q.Revision > 9007199254740991 || len(q.Entries) > 1000 || q.PositionMs < 0 || q.PositionMs > 86400000 {
		return invalid
	}
	if q.PlayMode != "repeat-all" && q.PlayMode != "repeat-one" && q.PlayMode != "shuffle" {
		return invalid
	}
	seen := map[string]bool{}
	for _, e := range q.Entries {
		if !entryID.MatchString(e.QueueEntryID) || seen[e.QueueEntryID] || ValidateTrack(e.Track) != nil {
			return invalid
		}
		seen[e.QueueEntryID] = true
	}
	if len(q.Entries) == 0 {
		if q.CurrentEntryID != nil || q.CurrentIndex != -1 || q.PositionMs != 0 {
			return invalid
		}
	} else if q.CurrentEntryID == nil || q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Entries) || q.Entries[q.CurrentIndex].QueueEntryID != *q.CurrentEntryID {
		return invalid
	}
	return nil
}
