package appservice

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
)

// ListLocalHistoryBefore continues from a stable timestamp/entry-ID cursor.
func (s *AppService) ListLocalHistoryBefore(limit int, beforePlayedAt int64, beforeEntryID string) (domain.HistoryPage, error) {
	if err := domain.ValidateHistoryPagination(limit, beforePlayedAt, beforeEntryID); err != nil {
		return domain.HistoryPage{}, err
	}
	library, ok := s.options.Library.(interface {
		ListLocalHistoryBefore(int, int64, string) (domain.HistoryPage, error)
	})
	if !ok {
		return domain.HistoryPage{}, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	page, err := library.ListLocalHistoryBefore(limit, beforePlayedAt, beforeEntryID)
	return page, storageError(err)
}
