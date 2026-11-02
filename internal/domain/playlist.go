package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxPlaylistInteger int64 = 9007199254740991
const MaxPlaylistTracks = 1000

type LocalPlaylist struct {
	PlaylistID string          `json:"playlistId"`
	Name       string          `json:"name"`
	Tracks     []TrackSnapshot `json:"tracks"`
	Revision   int64           `json:"revision"`
	CreatedAt  int64           `json:"createdAt"`
	UpdatedAt  int64           `json:"updatedAt"`
}

type LocalPlaylistSummary struct {
	PlaylistID string `json:"playlistId"`
	Name       string `json:"name"`
	TrackCount int    `json:"trackCount"`
	Revision   int64  `json:"revision"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type PlaylistUpdate struct {
	PlaylistID       string          `json:"playlistId"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Name             string          `json:"name"`
	Tracks           []TrackSnapshot `json:"tracks"`
}

func ValidatePlaylistID(id string) error {
	if !entryID.MatchString(id) {
		return errors.New("INVALID_PLAYLIST_ID")
	}
	return nil
}

func NormalizePlaylistName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || len(name) > 512 || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", errors.New("INVALID_PLAYLIST_NAME")
	}
	return name, nil
}

func ValidatePlaylistRevision(revision int64) error {
	if revision < 1 || revision > MaxPlaylistInteger {
		return errors.New("INVALID_PLAYLIST_REVISION")
	}
	return nil
}

func ValidatePlaylistTracks(tracks []TrackSnapshot) error {
	if len(tracks) > MaxPlaylistTracks {
		return errors.New("INVALID_PLAYLIST_TRACKS")
	}
	seen := make(map[string]bool, len(tracks))
	for _, track := range tracks {
		if seen[track.NeteaseSongID] || ValidateTrack(track) != nil {
			return errors.New("INVALID_PLAYLIST_TRACKS")
		}
		seen[track.NeteaseSongID] = true
	}
	return nil
}

func ValidatePlaylistUpdate(update PlaylistUpdate) error {
	if err := ValidatePlaylistID(update.PlaylistID); err != nil {
		return err
	}
	if err := ValidatePlaylistRevision(update.ExpectedRevision); err != nil {
		return err
	}
	if _, err := NormalizePlaylistName(update.Name); err != nil {
		return err
	}
	return ValidatePlaylistTracks(update.Tracks)
}

func ValidatePlaylistSummary(p LocalPlaylistSummary) error {
	if err := ValidatePlaylistID(p.PlaylistID); err != nil {
		return err
	}
	name, err := NormalizePlaylistName(p.Name)
	if err != nil || name != p.Name {
		return errors.New("INVALID_PLAYLIST_NAME")
	}
	if err = ValidatePlaylistRevision(p.Revision); err != nil {
		return err
	}
	if p.CreatedAt < 1 || p.CreatedAt > MaxPlaylistInteger || p.UpdatedAt < p.CreatedAt || p.UpdatedAt > MaxPlaylistInteger || p.TrackCount < 0 || p.TrackCount > MaxPlaylistTracks {
		return errors.New("INVALID_PLAYLIST_METADATA")
	}
	return nil
}

func ValidateLocalPlaylist(p LocalPlaylist) error {
	if err := ValidatePlaylistSummary(LocalPlaylistSummary{PlaylistID: p.PlaylistID, Name: p.Name, TrackCount: len(p.Tracks), Revision: p.Revision, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}); err != nil {
		return err
	}
	return ValidatePlaylistTracks(p.Tracks)
}
