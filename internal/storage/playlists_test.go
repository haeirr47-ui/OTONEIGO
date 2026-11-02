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
	"sync/atomic"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestPlaylistCRUDDurabilityCASAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	track := snapshot(1).Entries[0].Track
	other := track
	other.NeteaseSongID = "2"
	if err = s.SaveQueue(snapshot(1)); err != nil {
		t.Fatal(err)
	}
	if err = s.SetLocalFavorite(track, true); err != nil {
		t.Fatal(err)
	}
	if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "history", Track: track, PlayedAt: 1}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateLocalPlaylist("  离线歌曲  ")
	if err != nil || p.Name != "离线歌曲" || p.Revision != 1 || p.Tracks == nil || len(p.PlaylistID) != 32 {
		t.Fatal(p, err)
	}
	update := domain.PlaylistUpdate{PlaylistID: p.PlaylistID, ExpectedRevision: 1, Name: "renamed", Tracks: []domain.TrackSnapshot{track, other}}
	p, err = s.UpdateLocalPlaylist(update)
	if err != nil || p.Revision != 2 {
		t.Fatal(p, err)
	}
	update.ExpectedRevision = p.Revision
	update.Tracks = []domain.TrackSnapshot{other, track}
	p, err = s.UpdateLocalPlaylist(update)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateLocalPlaylist(update); !errors.Is(err, ErrStalePlaylistRevision) {
		t.Fatal("stale update", err)
	}
	if err = s.DeleteLocalPlaylist(p.PlaylistID, 2); !errors.Is(err, ErrStalePlaylistRevision) {
		t.Fatal("stale delete", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetLocalPlaylist(p.PlaylistID)
	if err != nil || !reflect.DeepEqual(got, p) || got.Tracks[0].NeteaseSongID != "2" {
		t.Fatal(got, err)
	}
	list, err := s.ListLocalPlaylists()
	if err != nil || len(list) != 1 || list[0].TrackCount != 2 || list[0].Revision != 3 {
		t.Fatal(list, err)
	}
	if err = s.DeleteLocalPlaylist(p.PlaylistID, p.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetLocalPlaylist(p.PlaylistID); err == nil || err.Error() != "PLAYLIST_NOT_FOUND" {
		t.Fatal(err)
	}
	list, err = s.ListLocalPlaylists()
	if err != nil || list == nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	q, err := s.LoadQueue()
	if err != nil || q.Revision != 1 {
		t.Fatal(q, err)
	}
	f, err := s.GetLocalFavorites()
	if err != nil || len(f) != 1 {
		t.Fatal(f, err)
	}
	h, err := s.ListLocalHistory(1, 0)
	if err != nil || len(h) != 1 {
		t.Fatal(h, err)
	}
}

func TestPlaylistConcurrentCASAndLimits(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateLocalPlaylist("CAS")
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.UpdateLocalPlaylist(domain.PlaylistUpdate{PlaylistID: p.PlaylistID, ExpectedRevision: 1, Name: "winner"})
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrStalePlaylistRevision) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("CAS winners", successes.Load())
	}
	for i := 1; i < 100; i++ {
		if _, err = s.CreateLocalPlaylist(fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.CreateLocalPlaylist("overflow"); err == nil || err.Error() != "PLAYLIST_LIMIT_REACHED" {
		t.Fatal(err)
	}
	list, err := s.ListLocalPlaylists()
	if err != nil || len(list) != 100 {
		t.Fatal(len(list), err)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].UpdatedAt < list[i].UpdatedAt || (list[i-1].UpdatedAt == list[i].UpdatedAt && list[i-1].PlaylistID > list[i].PlaylistID) {
			t.Fatal("unstable order")
		}
	}
	p, err = s.GetLocalPlaylist(p.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	track := snapshot(1).Entries[0].Track
	update := domain.PlaylistUpdate{PlaylistID: p.PlaylistID, ExpectedRevision: p.Revision, Name: p.Name, Tracks: []domain.TrackSnapshot{track, track}}
	if _, err = s.UpdateLocalPlaylist(update); err == nil {
		t.Fatal("duplicate accepted")
	}
	update.Tracks = make([]domain.TrackSnapshot, 1000)
	for i := range update.Tracks {
		update.Tracks[i] = track
		update.Tracks[i].NeteaseSongID = fmt.Sprint(i + 1)
	}
	if _, err = s.UpdateLocalPlaylist(update); err != nil {
		t.Fatal("1000-track boundary", err)
	}
	update.ExpectedRevision++
	update.Tracks = append(update.Tracks, track)
	if _, err = s.UpdateLocalPlaylist(update); err == nil {
		t.Fatal("1001 tracks accepted")
	}
	update.Tracks = update.Tracks[:1000]
	for i := range update.Tracks {
		update.Tracks[i].Name = strings.Repeat("x", 1024)
		update.Tracks[i].Artists = []domain.Artist{{Name: strings.Repeat("a", 1024)}}
	}
	if _, err = s.UpdateLocalPlaylist(update); err == nil || err.Error() != "PLAYLIST_TOO_LARGE" {
		t.Fatal("oversized metadata", err)
	}
}

func TestPlaylistCorruptionCannotBeOverwritten(t *testing.T) {
	for _, kind := range []string{"unknown", "trailing", "identity", "oversize", "count", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			s, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			p, err := s.CreateLocalPlaylist("original")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(p)
			switch kind {
			case "unknown":
				data = []byte(strings.TrimSuffix(string(data), "}") + `,"token":"secret"}`)
			case "trailing":
				data = append(data, []byte(" {}")...)
			case "identity":
				p.PlaylistID = "another"
				data, _ = json.Marshal(p)
			case "oversize":
				data = []byte(strings.Repeat("x", maxPlaylistMetadataBytes+1))
			case "count":
				_, err = s.db.Exec("UPDATE local_playlists SET track_count=1001")
			case "overflow":
				p.Revision = domain.MaxPlaylistInteger
				data, _ = json.Marshal(p)
				_, err = s.db.Exec("UPDATE local_playlists SET revision=?", p.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("UPDATE local_playlists SET metadata=?", string(data)); err != nil {
				t.Fatal(err)
			}
			id := p.PlaylistID
			if kind == "identity" {
				var storedID string
				_ = s.db.QueryRow("SELECT playlist_id FROM local_playlists").Scan(&storedID)
				id = storedID
			}
			if kind != "overflow" {
				if _, err = s.GetLocalPlaylist(id); err == nil {
					t.Fatal("corrupt get accepted")
				}
				if _, err = s.ListLocalPlaylists(); err == nil {
					t.Fatal("corrupt list accepted")
				}
				if err = s.DeleteLocalPlaylist(id, 1); err == nil {
					t.Fatal("corrupt delete accepted")
				}
			}
			if _, err = s.UpdateLocalPlaylist(domain.PlaylistUpdate{PlaylistID: id, ExpectedRevision: p.Revision, Name: "replacement"}); err == nil {
				t.Fatal("corrupt/overflow overwritten")
			}
			var stored string
			if err = s.db.QueryRow("SELECT metadata FROM local_playlists").Scan(&stored); err != nil || stored != string(data) {
				t.Fatal("metadata changed", err)
			}
		})
	}
}

func TestSchemaTwoPlaylistMigrationPreservesLibraryAndRollback(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v2.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			track := snapshot(1).Entries[0].Track
			if err = s.SaveQueue(snapshot(7)); err != nil {
				t.Fatal(err)
			}
			if err = s.SetSettings(domain.DefaultSettings()); err != nil {
				t.Fatal(err)
			}
			if err = s.SetLocalFavorite(track, true); err != nil {
				t.Fatal(err)
			}
			if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "keep", Track: track, PlayedAt: 5}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("DROP TABLE local_playlists"); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{"DROP TABLE search_history", "ALTER TABLE local_favorites DROP COLUMN search_text", "ALTER TABLE local_history DROP COLUMN search_text"} {
				if _, err = s.db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			// Restore the actual v2 favorites shape before replaying migrations.
			if _, err = s.db.Exec("DROP INDEX local_favorites_order"); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("ALTER TABLE local_favorites DROP COLUMN added_order"); err != nil {
				t.Fatal(err)
			}
			if collision {
				if _, err = s.db.Exec("CREATE INDEX local_playlists_order ON local_favorites(song_id)"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.db.Exec("PRAGMA user_version=2"); err != nil {
				t.Fatal(err)
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
				_ = db.QueryRow("PRAGMA user_version").Scan(&version)
				_ = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='local_playlists'").Scan(&count)
				if version != 2 || count != 0 {
					t.Fatal("partial migration", version, count)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			q, e := s.LoadQueue()
			if e != nil || q.Revision != 7 {
				t.Fatal(q, e)
			}
			settings, e := s.GetSettings()
			if e != nil || settings != domain.DefaultSettings() {
				t.Fatal(settings, e)
			}
			f, e := s.GetLocalFavorites()
			if e != nil || len(f) != 1 {
				t.Fatal(f, e)
			}
			h, e := s.ListLocalHistory(1, 0)
			if e != nil || len(h) != 1 {
				t.Fatal(h, e)
			}
			if _, err = s.CreateLocalPlaylist("new"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
