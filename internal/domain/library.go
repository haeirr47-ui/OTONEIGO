package domain

import "errors"

type HistoryEntry struct {
	HistoryEntryID string        `json:"historyEntryId"`
	Track          TrackSnapshot `json:"track"`
	PlayedAt       int64         `json:"playedAt"`
	ListenedMs     int64         `json:"listenedMs,omitempty"`
}

func ValidateHistory(h HistoryEntry) error {
	if !entryID.MatchString(h.HistoryEntryID) || h.PlayedAt < 1 || h.PlayedAt > 9007199254740991 || h.ListenedMs < 0 || h.ListenedMs > 86400000 {
		return errors.New("INVALID_HISTORY_ENTRY")
	}
	return ValidateTrack(h.Track)
}
