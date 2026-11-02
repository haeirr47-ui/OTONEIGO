package storage

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"path/filepath"
	"sync"
	"testing"
)

func snapshot(rev int64) domain.QueueSnapshot {
	id := "entry-1"
	return domain.QueueSnapshot{Revision: rev, Entries: []domain.QueueEntry{{QueueEntryID: id, Track: domain.TrackSnapshot{NeteaseSongID: "9007199254740993", Name: "offline title", Artists: []domain.Artist{{Name: "artist"}}, DurationMs: 123000}}}, CurrentEntryID: &id, CurrentIndex: 0, PlayMode: "repeat-all", PositionMs: 42}
}
func TestDurableReopenAndStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-app", "data", "queue.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.LoadQueue()
	if e != nil || q != nil {
		t.Fatal("fresh queue must be nil", e)
	}
	if e = s.SaveQueue(snapshot(2)); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(s.SaveQueue(snapshot(2)), ErrStaleRevision) || !errors.Is(s.SaveQueue(snapshot(1)), ErrStaleRevision) {
		t.Fatal("stale revision accepted")
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	q, e = s.LoadQueue()
	if e != nil || q.Revision != 2 || q.Entries[0].Track.Name != "offline title" || q.Entries[0].Track.NeteaseSongID != "9007199254740993" {
		t.Fatal("reopen failed", e)
	}
}
func TestConcurrentRevisionAndValidation(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "queue.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 1; i <= 30; i++ {
		wg.Add(1)
		go func(rev int64) {
			defer wg.Done()
			if e := s.SaveQueue(snapshot(rev)); e != nil && !errors.Is(e, ErrStaleRevision) {
				t.Error(e)
			}
		}(int64(i))
	}
	wg.Wait()
	q, e := s.LoadQueue()
	if e != nil || q.Revision != 30 {
		t.Fatal("lost highest revision", e)
	}
	for _, mutate := range []func(*domain.QueueSnapshot){func(q *domain.QueueSnapshot) { q.Entries[0].Track.CoverRef = "https://p1.music.126.net/a?token=secret" }, func(q *domain.QueueSnapshot) { q.CurrentIndex = 2 }, func(q *domain.QueueSnapshot) { q.Entries = append(q.Entries, q.Entries[0]) }, func(q *domain.QueueSnapshot) { q.Entries[0].Track.NeteaseSongID = "1e20" }} {
		bad := snapshot(31)
		mutate(&bad)
		if s.SaveQueue(bad) == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
func TestUnsupportedMigrationDoesNotHang(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("PRAGMA user_version=8"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if future, e := Open(path); e == nil {
		future.Close()
		t.Fatal("future schema accepted")
	}
}
