package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"strings"
	"time"
)

func trackSearchText(t domain.TrackSnapshot) string {
	parts := []string{t.Name}
	for _, a := range t.Artists {
		parts = append(parts, a.Name)
	}
	if t.Album != nil {
		parts = append(parts, t.Album.Name)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

// Batches release their rows before updating on the single SQLite connection.
// All schema changes and derived values belong to the caller's transaction.
func migrateLibraryQuery(tx *sql.Tx) error {
	for _, q := range []string{
		"ALTER TABLE local_favorites ADD COLUMN search_text TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE local_history ADD COLUMN search_text TEXT NOT NULL DEFAULT ''",
		"CREATE TABLE search_history (canonical_query TEXT PRIMARY KEY, query TEXT NOT NULL, searched_at INTEGER NOT NULL)",
		"CREATE INDEX search_history_order ON search_history(searched_at DESC, canonical_query)",
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	for _, table := range []struct {
		name, id string
		history  bool
	}{{"local_favorites", "song_id", false}, {"local_history", "entry_id", true}} {
		last := ""
		firstBatch := true
		valueColumn := "added_order"
		if table.history {
			valueColumn = "played_at"
		}
		for {
			query := "SELECT " + table.id + ",metadata," + valueColumn + " FROM " + table.name
			args := []any{}
			// The initial batch deliberately has no lower bound: TEXT primary
			// keys in existing SQLite tables can contain NULL or empty values.
			if !firstBatch {
				query += " WHERE " + table.id + ">?"
				args = append(args, last)
			}
			query += " ORDER BY " + table.id + " LIMIT 100"
			rows, err := tx.Query(query, args...)
			if err != nil {
				return err
			}
			type item struct{ id, text string }
			batch := []item{}
			for rows.Next() {
				var id, data string
				var storedValue int64
				if err = rows.Scan(&id, &data, &storedValue); err != nil {
					rows.Close()
					return err
				}
				var track domain.TrackSnapshot
				if table.history {
					var h domain.HistoryEntry
					if err = decodeLocalMetadata(data, &h); err == nil {
						if h.HistoryEntryID != id || h.PlayedAt != storedValue || domain.ValidateHistory(h) != nil {
							err = errors.New("INVALID_STORED_HISTORY")
						}
					}
					track = h.Track
				} else {
					err = decodeLocalMetadata(data, &track)
					if err == nil && (track.NeteaseSongID != id || domain.ValidateTrack(track) != nil || storedValue < 1 || storedValue > 9007199254740991) {
						err = errors.New("INVALID_STORED_FAVORITE")
					}
				}
				if err != nil {
					rows.Close()
					return err
				}
				batch = append(batch, item{id, trackSearchText(track)})
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if len(batch) == 0 {
				break
			}
			for _, v := range batch {
				if _, err = tx.Exec("UPDATE "+table.name+" SET search_text=? WHERE "+table.id+"=?", v.text, v.id); err != nil {
					return err
				}
			}
			last = batch[len(batch)-1].id
			firstBatch = false
		}
	}
	_, err := tx.Exec("PRAGMA user_version=5")
	return err
}

func (s *Store) SearchLocalFavorites(ctx context.Context, query string, limit int, beforeAddedOrder int64, beforeSongID string) (domain.FavoritePage, error) {
	query, err := domain.NormalizeLocalQuery(query)
	if err != nil {
		return domain.FavoritePage{}, err
	}
	if err = domain.ValidateFavoritePagination(limit, beforeAddedOrder, beforeSongID); err != nil {
		return domain.FavoritePage{}, err
	}
	suffix := " WHERE instr(search_text,?)>0"
	args := []any{strings.ToLower(query)}
	if beforeAddedOrder != 0 {
		suffix += " AND (added_order<? OR (added_order=? AND song_id>?))"
		args = append(args, beforeAddedOrder, beforeAddedOrder, beforeSongID)
	}
	suffix += " ORDER BY added_order DESC,song_id ASC"
	records, err := s.readLibrary(ctx, favoriteTable, suffix, args, limit+1, 0)
	if err != nil {
		return domain.FavoritePage{}, err
	}
	page := domain.FavoritePage{Tracks: []domain.TrackSnapshot{}, HasMore: len(records) > limit}
	if page.HasMore {
		records = records[:limit]
	}
	for _, r := range records {
		page.Tracks = append(page.Tracks, r.track)
		page.NextAddedOrder = r.value
		page.NextSongID = r.id
	}
	return page, nil
}

func (s *Store) SearchLocalHistory(ctx context.Context, query string, limit int, beforePlayedAt int64, beforeEntryID string) (domain.HistoryPage, error) {
	query, err := domain.NormalizeLocalQuery(query)
	if err != nil {
		return domain.HistoryPage{}, err
	}
	if err = domain.ValidateHistoryPagination(limit, beforePlayedAt, beforeEntryID); err != nil {
		return domain.HistoryPage{}, err
	}
	suffix := " WHERE instr(search_text,?)>0"
	args := []any{strings.ToLower(query)}
	if beforePlayedAt != 0 {
		suffix += " AND (played_at,entry_id)<(?,?)"
		args = append(args, beforePlayedAt, beforeEntryID)
	}
	suffix += " ORDER BY played_at DESC,entry_id DESC"
	records, err := s.readLibrary(ctx, historyTable, suffix, args, limit+1, 0)
	if err != nil {
		return domain.HistoryPage{}, err
	}
	entries := historyRecords(records)
	page := domain.HistoryPage{Entries: entries, HasMore: len(entries) > limit}
	if page.HasMore {
		page.Entries = entries[:limit]
	}
	return page, nil
}

func (s *Store) GetLocalFavoriteIDs(ctx context.Context) ([]string, error) {
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT rowid,CASE WHEN typeof(song_id)='text' AND length(CAST(song_id AS BLOB))<=1024 THEN song_id ELSE NULL END FROM local_favorites ORDER BY song_id")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	bad := []int64{}
	for rows.Next() {
		var rowID int64
		var raw any
		if err = rows.Scan(&rowID, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		id, valid := raw.(string)
		if !valid || !domain.ValidID(id) {
			bad = append(bad, rowID)
		} else {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range bad {
		if err = quarantineRecord(tx, favoriteTable, id, true); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) ListSearchHistory(ctx context.Context, limit int) ([]domain.SearchHistoryEntry, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("INVALID_SEARCH_HISTORY_LIMIT")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT query,searched_at FROM search_history ORDER BY searched_at DESC,canonical_query LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []domain.SearchHistoryEntry{}
	for rows.Next() {
		var e domain.SearchHistoryEntry
		if err = rows.Scan(&e.Query, &e.SearchedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
func (s *Store) RecordSearchQuery(ctx context.Context, query string) error {
	query, err := domain.NormalizeLocalQuery(query)
	if err != nil {
		return err
	}
	if query == "" {
		return errors.New("INVALID_SEARCH_QUERY")
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	_, err = s.db.ExecContext(ctx, "INSERT INTO search_history(canonical_query,query,searched_at) VALUES(?,?,MAX(?,COALESCE((SELECT MAX(searched_at)+1 FROM search_history),0))) ON CONFLICT(canonical_query) DO UPDATE SET query=excluded.query,searched_at=excluded.searched_at", strings.ToLower(query), query, time.Now().UnixMilli())
	return err
}
func (s *Store) DeleteSearchQuery(ctx context.Context, query string) error {
	query, err := domain.NormalizeLocalQuery(query)
	if err != nil {
		return err
	}
	if query == "" {
		return errors.New("INVALID_SEARCH_QUERY")
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	_, err = s.db.ExecContext(ctx, "DELETE FROM search_history WHERE canonical_query=?", strings.ToLower(query))
	return err
}
func (s *Store) ClearSearchHistory(ctx context.Context) error {
	s.writer.Lock()
	defer s.writer.Unlock()
	_, err := s.db.ExecContext(ctx, "DELETE FROM search_history")
	return err
}
