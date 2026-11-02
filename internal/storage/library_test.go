package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestVersionOneMigrationPreservesQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot(42))
	for _, query := range []string{"CREATE TABLE queue_snapshot (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, metadata TEXT NOT NULL)", "PRAGMA user_version=1"} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO queue_snapshot VALUES(1,42,?)", string(data)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, err := s.LoadQueue()
	if err != nil || !reflect.DeepEqual(*q, snapshot(42)) {
		t.Fatalf("queue migration: %v %v", q, err)
	}
	if !errors.Is(s.SaveQueue(snapshot(42)), ErrStaleRevision) {
		t.Fatal("migration broke CAS")
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 7 {
		t.Fatal(version, err)
	}
	settings, err := s.GetSettings()
	if err != nil || settings != domain.DefaultSettings() {
		t.Fatal(settings, err)
	}
}

func TestMigrationRollbackAndFutureSchema(t *testing.T) {
	for _, future := range []bool{false, true} {
		t.Run(fmt.Sprint(future), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "migration.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			version := 1
			if future {
				version = 8
			}
			if _, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
				t.Fatal(err)
			}
			// A collision after local_settings creation exercises rollback of DDL.
			if _, err = db.Exec("CREATE TABLE local_favorites (sentinel TEXT)"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if s, err := Open(path); err == nil {
				s.Close()
				t.Fatal("invalid migration accepted")
			}
			db, err = sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var actual, count int
			if err = db.QueryRow("PRAGMA user_version").Scan(&actual); err != nil || actual != version {
				t.Fatal(actual, err)
			}
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('local_settings','local_history')").Scan(&count); err != nil || count != 0 {
				t.Fatal("partial migration", count, err)
			}
		})
	}
}

func TestSettingsDefaultAndPersistedQuality(t *testing.T) {
	for _, quality := range []string{"", "standard", "higher", "exhigh", "lossless", "hires"} {
		name := quality
		if name == "" {
			name = "no_settings"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			want := domain.DefaultSettings()
			if quality != "" {
				want = domain.LocalSettings{PreferredQuality: quality, RegionMode: "off", Volume: 0.3}
				// Seed the existing stored representation, including rows written
				// before gdEnabled was present, without applying today's defaults.
				data := fmt.Sprintf(`{"preferredQuality":%q,"regionMode":"off","volume":0.3}`, quality)
				if _, err = s.db.Exec("INSERT INTO local_settings(id,metadata) VALUES(1,?)", data); err != nil {
					s.Close()
					t.Fatal(err)
				}
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.GetSettings()
			if err != nil || got != want {
				t.Fatalf("settings = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestLocalLibraryDurableAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	track := snapshot(1).Entries[0].Track
	settings := domain.LocalSettings{PreferredQuality: "exhigh", RegionMode: "off", Volume: 0.3}
	if err = s.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err = s.SetLocalFavorite(track, true); err != nil {
		t.Fatal(err)
	}
	track.Name = "updated offline title"
	if err = s.SetLocalFavorite(track, true); err != nil {
		t.Fatal(err)
	}
	entry := domain.HistoryEntry{HistoryEntryID: "play-a", Track: track, PlayedAt: 100, ListenedMs: 1000}
	if err = s.AddLocalHistory(entry); err != nil {
		t.Fatal(err)
	}
	retry := entry
	retry.Track.Name = "retry must not replace metadata"
	retry.PlayedAt = 200
	if err = s.AddLocalHistory(retry); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	gotSettings, err := s.GetSettings()
	if err != nil || gotSettings != settings {
		t.Fatal(gotSettings, err)
	}
	favorites, err := s.GetLocalFavorites()
	if err != nil || len(favorites) != 1 || !reflect.DeepEqual(favorites[0], track) {
		t.Fatal(favorites, err)
	}
	history, err := s.ListLocalHistory(200, 0)
	if err != nil || len(history) != 1 || !reflect.DeepEqual(history[0], entry) {
		t.Fatal(history, err)
	}
	if err = s.SetLocalFavorite(track, false); err != nil {
		t.Fatal(err)
	}
	favorites, err = s.GetLocalFavorites()
	if err != nil || len(favorites) != 0 || favorites == nil {
		t.Fatal(favorites, err)
	}
	if err = s.ClearLocalHistory(); err != nil {
		t.Fatal(err)
	}
	history, err = s.ListLocalHistory(1, 0)
	if err != nil || len(history) != 0 || history == nil {
		t.Fatal(history, err)
	}
}

func TestLocalMetadataValidation(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, mutate := range []func(*domain.TrackSnapshot){
		func(t *domain.TrackSnapshot) { t.NeteaseSongID = "1e20" },
		func(t *domain.TrackSnapshot) { t.Name = strings.Repeat("x", 1025) },
		func(t *domain.TrackSnapshot) { t.CoverRef = "https://p1.music.126.net/image.jpg?token=secret" },
		func(t *domain.TrackSnapshot) { t.CoverRef = "https://m701.music.126.net/song.mp3?cookie=secret" },
	} {
		track := snapshot(1).Entries[0].Track
		mutate(&track)
		if s.SetLocalFavorite(track, true) == nil || s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "bad", Track: track, PlayedAt: 1}) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	for _, entry := range []domain.HistoryEntry{
		{HistoryEntryID: "bad/id", Track: snapshot(1).Entries[0].Track, PlayedAt: 1},
		{HistoryEntryID: "bad", Track: snapshot(1).Entries[0].Track},
		{HistoryEntryID: "bad", Track: snapshot(1).Entries[0].Track, PlayedAt: 1, ListenedMs: -1},
	} {
		if s.AddLocalHistory(entry) == nil {
			t.Fatal("invalid history accepted")
		}
	}
	for _, page := range [][2]int{{0, 0}, {201, 0}, {1, -1}, {1, 9007199254740992}} {
		if _, err = s.ListLocalHistory(page[0], page[1]); err == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
	if err = s.SetSettings(domain.LocalSettings{PreferredQuality: "bad"}); err == nil {
		t.Fatal("invalid settings accepted")
	}
	// Persisted unknown fields are tolerated but never enter the typed settings value.
	if _, err = s.db.Exec(`INSERT INTO local_settings VALUES(1,'{"preferredQuality":"standard","regionMode":"auto","volume":1,"token":"secret"}')`); err != nil {
		t.Fatal(err)
	}
	if got, e := s.GetSettings(); e != nil || got.PreferredQuality != "standard" {
		t.Fatal("known settings projection unavailable", e)
	}
}

func TestHistoryLongTermRetentionAndOrdering(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	track := snapshot(1).Entries[0].Track
	// Seed the former boundary efficiently; production insertion must retain it.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		entry := domain.HistoryEntry{HistoryEntryID: fmt.Sprintf("play-%04d", i), Track: track, PlayedAt: int64(i + 1)}
		data, _ := encodeLocalMetadata(entry)
		if _, err = tx.Exec("INSERT INTO local_history(entry_id,played_at,metadata) VALUES(?,?,?)", entry.HistoryEntryID, entry.PlayedAt, data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	entry := domain.HistoryEntry{HistoryEntryID: "latest-z", Track: track, PlayedAt: 5000}
	if err = s.AddLocalHistory(entry); err != nil {
		t.Fatal(err)
	}
	history, err := s.ListLocalHistory(2, 0)
	if err != nil || len(history) != 2 || history[0].HistoryEntryID != "play-4999" || history[1].HistoryEntryID != "latest-z" {
		t.Fatal(history, err)
	}
	history, err = s.ListLocalHistory(2, 4999)
	if err != nil || len(history) != 2 || history[1].HistoryEntryID != "play-0000" {
		t.Fatal(history, err)
	}
	// A delete failure cannot affect ordinary additions: they perform no pruning.
	if _, err = s.db.Exec("CREATE TRIGGER reject_prune BEFORE DELETE ON local_history BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	entry.HistoryEntryID = "retained-beyond-5000"
	entry.PlayedAt = 6000
	if err = s.AddLocalHistory(entry); err != nil {
		t.Fatal("addition attempted to prune", err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM local_history").Scan(&count); err != nil || count != 5002 {
		t.Fatal("history lost", count, err)
	}
	history, err = s.ListLocalHistory(2, 5001)
	if err != nil || len(history) != 1 || history[0].HistoryEntryID != "play-0000" {
		t.Fatal("offset above previous cap", history, err)
	}
}

func TestConcurrentLocalWrites(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 1; i <= 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			track := snapshot(1).Entries[0].Track
			track.NeteaseSongID = fmt.Sprint(i)
			if err := s.SetLocalFavorite(track, true); err != nil {
				t.Error(err)
			}
			if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: fmt.Sprintf("play-%d", i), Track: track, PlayedAt: int64(i)}); err != nil {
				t.Error(err)
			}
			if err := s.SetSettings(domain.DefaultSettings()); err != nil {
				t.Error(err)
			}
			if err := s.SaveQueue(snapshot(int64(i))); err != nil && !errors.Is(err, ErrStaleRevision) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	favorites, err := s.GetLocalFavorites()
	if err != nil || len(favorites) != 30 {
		t.Fatal(len(favorites), err)
	}
	history, err := s.ListLocalHistory(200, 0)
	if err != nil || len(history) != 30 {
		t.Fatal(len(history), err)
	}
	q, err := s.LoadQueue()
	if err != nil || q.Revision != 30 {
		t.Fatal(q, err)
	}
}
