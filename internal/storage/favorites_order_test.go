package storage

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestFavoritesAdditionOrderSurvivesMetadataUpdatesAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	track := snapshot(1).Entries[0].Track
	for _, id := range []string{"20", "3", "10"} {
		track.NeteaseSongID = id
		if err = s.SetLocalFavorite(track, true); err != nil {
			t.Fatal(err)
		}
	}
	track.NeteaseSongID = "20"
	track.Name = "updated"
	if err = s.SetLocalFavorite(track, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertOrder := func(want []string) {
		t.Helper()
		tracks, e := s.GetLocalFavorites()
		if e != nil {
			t.Fatal(e)
		}
		got := []string{}
		for _, tr := range tracks {
			got = append(got, tr.NeteaseSongID)
			if tr.NeteaseSongID == "20" && tr.Name != "updated" {
				t.Fatal("metadata update lost")
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
	assertOrder([]string{"10", "3", "20"})
	if err = s.SetLocalFavorite(track, false); err != nil {
		t.Fatal(err)
	}
	if err = s.SetLocalFavorite(track, true); err != nil {
		t.Fatal(err)
	}
	assertOrder([]string{"20", "10", "3"})
}

func TestSchemaThreeFavoriteMigrationPreservesLibraryAndRollsBack(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve", true: "rollback"}[collision], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v3.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			track := snapshot(1).Entries[0].Track
			for _, id := range []string{"20", "3", "10"} {
				track.NeteaseSongID = id
				if err = s.SetLocalFavorite(track, true); err != nil {
					t.Fatal(err)
				}
			}
			settings := domain.DefaultSettings()
			settings.Volume = .4
			if err = s.SetSettings(settings); err != nil {
				t.Fatal(err)
			}
			if err = s.SaveQueue(snapshot(7)); err != nil {
				t.Fatal(err)
			}
			if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "keep", Track: track, PlayedAt: 5}); err != nil {
				t.Fatal(err)
			}
			playlist, err := s.CreateLocalPlaylist("preserved")
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{"DROP TABLE search_history", "ALTER TABLE local_favorites DROP COLUMN search_text", "ALTER TABLE local_history DROP COLUMN search_text", "DROP INDEX local_favorites_order", "ALTER TABLE local_favorites DROP COLUMN added_order", "PRAGMA user_version=3"} {
				if _, err = s.db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if collision {
				if _, err = s.db.Exec("CREATE INDEX local_favorites_order ON local_favorites(song_id)"); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			s, err = Open(path)
			if collision {
				if err == nil {
					s.Close()
					t.Fatal("collision accepted")
				}
				db, e := sql.Open("sqlite", path)
				if e != nil {
					t.Fatal(e)
				}
				defer db.Close()
				var version, count int
				if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 3 {
					t.Fatal(version, e)
				}
				if e = db.QueryRow("SELECT count(*) FROM pragma_table_info('local_favorites') WHERE name='added_order'").Scan(&count); e != nil || count != 0 {
					t.Fatal("partial DDL", count, e)
				}
				if e = db.QueryRow("SELECT count(*) FROM local_favorites").Scan(&count); e != nil || count != 3 {
					t.Fatal("rows lost", count, e)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			favorites, e := s.GetLocalFavorites()
			if e != nil || len(favorites) != 3 || favorites[0].NeteaseSongID != "10" || favorites[1].NeteaseSongID != "20" || favorites[2].NeteaseSongID != "3" {
				t.Fatal(favorites, e)
			}
			q, e := s.LoadQueue()
			if e != nil || q.Revision != 7 {
				t.Fatal(q, e)
			}
			got, e := s.GetSettings()
			if e != nil || got != settings {
				t.Fatal(got, e)
			}
			h, e := s.ListLocalHistoryBefore(1, 0, "")
			if e != nil || len(h.Entries) != 1 {
				t.Fatal(h, e)
			}
			p, e := s.GetLocalPlaylist(playlist.PlaylistID)
			if e != nil || !reflect.DeepEqual(p, playlist) {
				t.Fatal(p, e)
			}
		})
	}
}
