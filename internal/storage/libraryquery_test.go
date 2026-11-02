package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/voici5986/otoneigo/internal/domain"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func queryStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func queryTrack(id, name string) domain.TrackSnapshot {
	t := snapshot(1).Entries[0].Track
	t.NeteaseSongID = id
	t.Name = name
	return t
}
func TestWholeLibrarySearchAndLiteralUnicode(t *testing.T) {
	s := queryStore(t)
	ctx := context.Background()
	for i := 1; i <= 225; i++ {
		tr := queryTrack(fmt.Sprint(i), "ordinary")
		if i == 1 {
			tr.Name = "中文 日本語 ÄPFEL 100%_literal"
			tr.Artists = []domain.Artist{{Name: "歌手"}}
			tr.Album = &domain.Album{Name: "专辑"}
		}
		if err := s.SetLocalFavorite(tr, true); err != nil {
			t.Fatal(err)
		}
		if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: fmt.Sprintf("entry_%03d", i), PlayedAt: int64(i), Track: tr}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.SearchLocalFavorites(ctx, "", 20, 0, "")
	if err != nil || len(first.Tracks) != 20 || !first.HasMore {
		t.Fatal(first, err)
	}
	ids, err := s.GetLocalFavoriteIDs(ctx)
	if err != nil || len(ids) != 225 {
		t.Fatal(len(ids), err)
	}
	for _, query := range []string{"中文", "日本語", "äpfel", "%_", "歌手", "专辑"} {
		p, e := s.SearchLocalFavorites(ctx, query, 10, 0, "")
		if e != nil || len(p.Tracks) != 1 || p.Tracks[0].NeteaseSongID != "1" {
			t.Fatal(query, p, e)
		}
		h, e := s.SearchLocalHistory(ctx, query, 10, 0, "")
		if e != nil || len(h.Entries) != 1 || h.Entries[0].Track.NeteaseSongID != "1" {
			t.Fatal(query, h, e)
		}
	}
	p, err := s.SearchLocalFavorites(ctx, "does-not-match", 10, 0, "")
	if err != nil || p.Tracks == nil || len(p.Tracks) != 0 {
		t.Fatal(p, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.SearchLocalFavorites(ctx, "", 10, 0, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = s.SearchLocalHistory(ctx, "", 10, 0, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = s.GetLocalFavoriteIDs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestFavoriteCursorTiesInsertionAndMetadataUpdates(t *testing.T) {
	s := queryStore(t)
	ctx := context.Background()
	for _, id := range []string{"10", "20", "30"} {
		if err := s.SetLocalFavorite(queryTrack(id, "old"), true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("UPDATE local_favorites SET added_order=7"); err != nil {
		t.Fatal(err)
	}
	first, err := s.SearchLocalFavorites(ctx, "", 1, 0, "")
	if err != nil || first.Tracks[0].NeteaseSongID != "10" || !first.HasMore {
		t.Fatal(first, err)
	}
	if err = s.SetLocalFavorite(queryTrack("40", "new"), true); err != nil {
		t.Fatal(err)
	}
	if err = s.SetLocalFavorite(queryTrack("20", "updated"), true); err != nil {
		t.Fatal(err)
	}
	next, err := s.SearchLocalFavorites(ctx, "", 2, first.NextAddedOrder, first.NextSongID)
	if err != nil || len(next.Tracks) != 2 || next.Tracks[0].NeteaseSongID != "20" || next.Tracks[1].NeteaseSongID != "30" || next.HasMore {
		t.Fatal(next, err)
	}
	old, err := s.SearchLocalFavorites(ctx, "old", 10, 0, "")
	if err != nil || len(old.Tracks) != 2 {
		t.Fatal(old, err)
	}
	for _, cursor := range []struct {
		order int64
		id    string
	}{{0, "10"}, {1, ""}, {9007199254740992, "10"}} {
		if _, err = s.SearchLocalFavorites(ctx, "", 10, cursor.order, cursor.id); err == nil {
			t.Fatal(cursor)
		}
	}
}
func TestSearchHistoryCursorAndFirstWriteWins(t *testing.T) {
	s := queryStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: id, PlayedAt: 10, Track: queryTrack("1", "match")}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.SearchLocalHistory(ctx, "match", 1, 0, "")
	if err != nil || first.Entries[0].HistoryEntryID != "c" {
		t.Fatal(first, err)
	}
	if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "z", PlayedAt: 11, Track: queryTrack("1", "match")}); err != nil {
		t.Fatal(err)
	}
	if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "a", PlayedAt: 20, Track: queryTrack("1", "changed")}); err != nil {
		t.Fatal(err)
	}
	next, err := s.SearchLocalHistory(ctx, "match", 2, 10, "c")
	if err != nil || len(next.Entries) != 2 || next.Entries[0].HistoryEntryID != "b" || next.Entries[1].HistoryEntryID != "a" || next.HasMore {
		t.Fatal(next, err)
	}
	changed, err := s.SearchLocalHistory(ctx, "changed", 2, 0, "")
	if err != nil || len(changed.Entries) != 0 {
		t.Fatal(changed, err)
	}
}
func TestKeywordHistoryPersistenceDedupAndManualRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keywords.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ctx := context.Background()
	for _, q := range []string{"  ÄPFEL  ", "中文", "äpfel"} {
		if err = s.RecordSearchQuery(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.ListSearchHistory(ctx, 100)
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	if entries[0].Query != "äpfel" {
		t.Fatal("latest query not first", entries)
	}
	found := false
	for _, e := range entries {
		if e.Query == "äpfel" {
			found = true
		}
		if e.SearchedAt < 1 {
			t.Fatal(e)
		}
	}
	if !found {
		t.Fatal(entries)
	}
	for _, q := range []string{" ", strings.Repeat("a", 513), string([]byte{255})} {
		if err = s.RecordSearchQuery(ctx, q); err == nil {
			t.Fatal("invalid accepted")
		}
	}
	for i := 0; i < 120; i++ {
		if err = s.RecordSearchQuery(ctx, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM search_history").Scan(&count); err != nil || count != 122 {
		t.Fatal(count, err)
	}
	if err = s.DeleteSearchQuery(ctx, " ÄPFEL "); err != nil {
		t.Fatal(err)
	}
	if err = s.ClearSearchHistory(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err = s.ListSearchHistory(ctx, 100)
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if _, err = s.ListSearchHistory(ctx, 101); err == nil {
		t.Fatal("unbounded list")
	}
}

func TestSchemaFourQueryMigrationPreservesHealthyMetadata(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v4.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= 205; i++ {
				tr := queryTrack(fmt.Sprint(i), "日本語 ÄPFEL")
				if err = s.SetLocalFavorite(tr, true); err != nil {
					t.Fatal(err)
				}
				if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: fmt.Sprintf("e_%03d", i), PlayedAt: int64(i), Track: tr}); err != nil {
					t.Fatal(err)
				}
			}
			settings := domain.DefaultSettings()
			settings.Volume = .2
			if err = s.SetSettings(settings); err != nil {
				t.Fatal(err)
			}
			if err = s.SaveQueue(snapshot(3)); err != nil {
				t.Fatal(err)
			}
			playlist, err := s.CreateLocalPlaylist("preserve")
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.GetLocalFavorites()
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{"DROP TABLE search_history", "ALTER TABLE local_favorites DROP COLUMN search_text", "ALTER TABLE local_history DROP COLUMN search_text", "PRAGMA user_version=4"} {
				if _, err = s.db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if corrupt {
				if _, err = s.db.Exec("UPDATE local_history SET metadata='{}' WHERE entry_id='e_205'"); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			after, err := s.GetLocalFavorites()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("metadata changed", err)
			}
			p, err := s.SearchLocalFavorites(context.Background(), "äpfel", 200, 0, "")
			if err != nil || len(p.Tracks) != 200 || !p.HasMore {
				t.Fatal(p, err)
			}
			h, err := s.SearchLocalHistory(context.Background(), "日本語", 200, 0, "")
			if err != nil || len(h.Entries) != 200 || !h.HasMore {
				t.Fatal(h, err)
			}
			got, err := s.GetSettings()
			if err != nil || got != settings {
				t.Fatal(got, err)
			}
			q, err := s.LoadQueue()
			if err != nil || q.Revision != 3 {
				t.Fatal(q, err)
			}
			pl, err := s.GetLocalPlaylist(playlist.PlaylistID)
			if err != nil || !reflect.DeepEqual(pl, playlist) {
				t.Fatal(pl, err)
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Logical damage is isolated before schema-5 backfills; reopening is idempotent.
func TestSchemaFourQueryMigrationQuarantinesInvalidRows(t *testing.T) {
	cases := []struct{ name, corrupt string }{
		{"favorite_null_id", "UPDATE local_favorites SET song_id=NULL"},
		{"favorite_empty_id", "UPDATE local_favorites SET song_id=''"},
		{"history_null_id", "UPDATE local_history SET entry_id=NULL"},
		{"history_empty_id", "UPDATE local_history SET entry_id=''"},
		{"history_timestamp_mismatch", "UPDATE local_history SET played_at=6"},
		{"favorite_zero_order", "UPDATE local_favorites SET added_order=0"},
		{"favorite_negative_order", "UPDATE local_favorites SET added_order=-1"},
		{"favorite_unsafe_order", "UPDATE local_favorites SET added_order=9007199254740992"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid-v4.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			tr := queryTrack("123", "valid metadata")
			if err = s.SetLocalFavorite(tr, true); err != nil {
				t.Fatal(err)
			}
			if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "entry", PlayedAt: 5, Track: tr}); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{"DROP TABLE search_history", "ALTER TABLE local_favorites DROP COLUMN search_text", "ALTER TABLE local_history DROP COLUMN search_text", "PRAGMA user_version=4", tc.corrupt} {
				if _, err = s.db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			for attempt := 0; attempt < 2; attempt++ {
				s, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				summary, e := s.RecoverySummary()
				if e != nil || summary.QuarantinedFavorites+summary.QuarantinedHistory != 1 {
					t.Fatal(summary, e)
				}
				favorites, e := s.GetLocalFavorites()
				if e != nil {
					t.Fatal(e)
				}
				history, e := s.ListLocalHistory(10, 0)
				if e != nil || len(favorites)+len(history) != 1 {
					t.Fatal(len(favorites), len(history), e)
				}
				s.Close()
			}
		})
	}
}
