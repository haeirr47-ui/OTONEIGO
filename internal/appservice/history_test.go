package appservice

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
)

type cursorFailureLibrary struct{ LocalLibrary }

func (cursorFailureLibrary) ListLocalHistoryBefore(int, int64, string) (domain.HistoryPage, error) {
	return domain.HistoryPage{}, errors.New("C:\\private\\token=secret")
}

func TestHistoryCursorService(t *testing.T) {
	s, store := openLibraryService(t, domain.Capabilities{})
	for _, id := range []string{"a", "b", "c"} {
		if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: id, Track: fixtureTrack("1", "track"), PlayedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.ListLocalHistoryBefore(2, 0, "")
	if err != nil || !p.HasMore || len(p.Entries) != 2 {
		t.Fatal(p, err)
	}
	p, err = s.ListLocalHistoryBefore(2, p.Entries[1].PlayedAt, p.Entries[1].HistoryEntryID)
	if err != nil || p.HasMore || len(p.Entries) != 1 || p.Entries[0].HistoryEntryID != "a" {
		t.Fatal(p, err)
	}
	if _, err = s.ListLocalHistoryBefore(1, 0, "a"); err == nil || err.Error() != "INVALID_HISTORY_PAGINATION" {
		t.Fatal(err)
	}
	store.Close()
	if _, err = s.ListLocalHistoryBefore(1, 0, ""); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatal(err)
	}
	for _, lib := range []LocalLibrary{nil, failingLibrary{}, cursorFailureLibrary{}} {
		service := New(nil, nil, Options{Library: lib})
		if _, err = service.ListLocalHistoryBefore(1, 0, ""); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
	}
}
