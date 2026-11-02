package storage

import (
	"sync"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestHistoryCursorSurvivesNewInsertionsAndClear(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	track := snapshot(1).Entries[0].Track
	add := func(id string, at int64) {
		t.Helper()
		if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: id, Track: track, PlayedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	add("a", 1)
	add("b", 2)
	add("c", 2)
	add("d", 3)
	first, err := s.ListLocalHistoryBefore(2, 0, "")
	if err != nil || !first.HasMore || len(first.Entries) != 2 || first.Entries[1].HistoryEntryID != "c" {
		t.Fatal(first, err)
	}
	add("new", 4)
	add("z", 2)
	last := first.Entries[1]
	next, err := s.ListLocalHistoryBefore(2, last.PlayedAt, last.HistoryEntryID)
	if err != nil || next.HasMore || len(next.Entries) != 2 || next.Entries[0].HistoryEntryID != "b" || next.Entries[1].HistoryEntryID != "a" {
		t.Fatal(next, err)
	}
	// Readers see a coherent SQL statement whether clear commits before or after it.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, e := s.ListLocalHistoryBefore(2, last.PlayedAt, last.HistoryEntryID)
			if e != nil || (len(page.Entries) != 0 && len(page.Entries) != 2) {
				t.Errorf("clear/read: %+v %v", page, e)
			}
		}()
	}
	if err := s.ClearLocalHistory(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	page, err := s.ListLocalHistoryBefore(2, last.PlayedAt, last.HistoryEntryID)
	if err != nil || page.HasMore || page.Entries == nil || len(page.Entries) != 0 {
		t.Fatal(page, err)
	}
}

func TestHistoryCursorValidation(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, c := range []struct {
		limit int
		at    int64
		id    string
	}{{0, 0, ""}, {201, 0, ""}, {1, 0, "a"}, {1, 1, ""}, {1, -1, "a"}, {1, 9007199254740992, "a"}, {1, 1, "bad/id"}} {
		if _, err := s.ListLocalHistoryBefore(c.limit, c.at, c.id); err == nil || err.Error() != "INVALID_HISTORY_PAGINATION" {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	if _, err := s.ListLocalHistoryBefore(200, 9007199254740991, "valid-id"); err != nil {
		t.Fatal(err)
	}
}
