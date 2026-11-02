package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type FavoritePage struct {
	Tracks         []TrackSnapshot `json:"tracks"`
	HasMore        bool            `json:"hasMore"`
	NextAddedOrder int64           `json:"nextAddedOrder"`
	NextSongID     string          `json:"nextSongId"`
}
type SearchHistoryEntry struct {
	Query      string `json:"query"`
	SearchedAt int64  `json:"searchedAt"`
}

// NormalizeLocalQuery permits an empty filter; recording requires a nonempty query.
func NormalizeLocalQuery(query string) (string, error) {
	if !utf8.ValidString(query) {
		return "", errors.New("INVALID_SEARCH_QUERY")
	}
	query = strings.TrimSpace(query)
	if len(query) > 512 {
		return "", errors.New("INVALID_SEARCH_QUERY")
	}
	return query, nil
}
func ValidateFavoritePagination(limit int, order int64, id string) error {
	initial := order == 0 && id == ""
	if limit < 1 || limit > 200 || (!initial && (order < 1 || order > 9007199254740991 || !ValidID(id))) {
		return errors.New("INVALID_FAVORITE_PAGINATION")
	}
	return nil
}
