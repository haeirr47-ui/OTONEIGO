package domain

import "errors"

type HistoryPage struct {
	Entries []HistoryEntry `json:"entries"`
	HasMore bool           `json:"hasMore"`
}

// ValidateHistoryPagination accepts either an initial page or a complete cursor.
func ValidateHistoryPagination(limit int, beforePlayedAt int64, beforeEntryID string) error {
	initial := beforePlayedAt == 0 && beforeEntryID == ""
	if limit < 1 || limit > 200 || (!initial && (beforePlayedAt < 1 || beforePlayedAt > 9007199254740991 || !entryID.MatchString(beforeEntryID))) {
		return errors.New("INVALID_HISTORY_PAGINATION")
	}
	return nil
}
