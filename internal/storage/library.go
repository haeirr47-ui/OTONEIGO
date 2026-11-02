package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/voici5986/otoneigo/internal/domain"
)

const (
	maxLibraryMetadataBytes = 128 * 1024
	// History is retained until explicitly cleared; each page remains bounded.
	maxHistoryPageSize = 200
)

func decodeLocalMetadata(data string, value any) error {
	if len(data) > maxLibraryMetadataBytes || !utf8.ValidString(data) {
		return errors.New("INVALID_STORED_LOCAL_METADATA")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(value); err != nil {
		return errors.New("INVALID_STORED_LOCAL_METADATA")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("INVALID_STORED_LOCAL_METADATA")
	}
	return nil
}

func encodeLocalMetadata(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(data) > maxLibraryMetadataBytes {
		return "", errors.New("LOCAL_METADATA_TOO_LARGE")
	}
	return string(data), nil
}

func (s *Store) GetSettings() (domain.LocalSettings, error) {
	var data string
	err := s.db.QueryRow("SELECT metadata FROM local_settings WHERE id=1").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DefaultSettings(), nil
	}
	if err != nil {
		return domain.LocalSettings{}, err
	}
	var settings domain.LocalSettings
	if err = decodeLocalMetadata(data, &settings); err != nil {
		return domain.LocalSettings{}, err
	}
	if err = domain.ValidateSettings(settings); err != nil {
		return domain.LocalSettings{}, errors.New("INVALID_STORED_SETTINGS")
	}
	return settings, nil
}

func (s *Store) SetSettings(settings domain.LocalSettings) error {
	if err := domain.ValidateSettings(settings); err != nil {
		return err
	}
	data, err := encodeLocalMetadata(settings)
	if err != nil {
		return err
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	var previous string
	err = s.db.QueryRow("SELECT metadata FROM local_settings WHERE id=1").Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var stored domain.LocalSettings
		if decodeLocalMetadata(previous, &stored) != nil || domain.ValidateSettings(stored) != nil {
			return errors.New("INVALID_STORED_SETTINGS")
		}
		data, err = mergeSettingsMetadata(previous, settings)
		if err != nil {
			return err
		}
	}
	_, err = s.db.Exec("INSERT INTO local_settings(id,metadata) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata", data)
	return err
}

// GetLocalFavorites returns the newest additions first without network hydration.
func (s *Store) GetLocalFavorites() ([]domain.TrackSnapshot, error) {
	records, err := s.readLibrary(context.Background(), favoriteTable, " ORDER BY added_order DESC,song_id", nil, 0, 0)
	if err != nil {
		return nil, err
	}
	tracks := []domain.TrackSnapshot{}
	for _, r := range records {
		tracks = append(tracks, r.track)
	}
	return tracks, nil
}

func (s *Store) SetLocalFavorite(track domain.TrackSnapshot, liked bool) error {
	if err := domain.ValidateTrack(track); err != nil {
		return err
	}
	data, err := encodeLocalMetadata(track)
	if err != nil {
		return err
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	if !liked {
		_, err = s.db.Exec("DELETE FROM local_favorites WHERE song_id=?", track.NeteaseSongID)
	} else {
		tx, e := s.db.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var raw, id sql.NullString
		var order sql.NullInt64
		e = tx.QueryRow("SELECT CASE WHEN typeof(metadata)='text' AND length(CAST(metadata AS BLOB))<=131072 THEN metadata ELSE NULL END,CASE WHEN typeof(song_id)='text' AND length(CAST(song_id AS BLOB))<=1024 THEN song_id ELSE NULL END,CASE WHEN typeof(added_order)='integer' THEN added_order ELSE NULL END FROM local_favorites WHERE song_id=?", track.NeteaseSongID).Scan(&raw, &id, &order)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			var stored domain.TrackSnapshot
			if !id.Valid || id.String != track.NeteaseSongID || !order.Valid || order.Int64 < 1 || order.Int64 > 9007199254740991 || !raw.Valid || decodeLocalMetadata(raw.String, &stored) != nil || domain.ValidateTrack(stored) != nil || stored.NeteaseSongID != track.NeteaseSongID {
				return errors.New("INVALID_STORED_FAVORITE")
			}
			decoder := json.NewDecoder(strings.NewReader(raw.String))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&stored) != nil {
				return errors.New("LOCAL_METADATA_REQUIRES_NEWER_VERSION")
			}
		}

		_, err = tx.Exec("INSERT INTO local_favorites(song_id,metadata,search_text,added_order) VALUES(?,?,?,(SELECT COALESCE(MAX(added_order),0)+1 FROM local_favorites)) ON CONFLICT(song_id) DO UPDATE SET metadata=excluded.metadata,search_text=excluded.search_text", track.NeteaseSongID, data, trackSearchText(track))
		if err == nil {
			err = tx.Commit()
		}
	}
	return err
}

// AddLocalHistory is idempotent by historyEntryId: retries preserve the original
// entry. Entries remain available until explicitly cleared.
func (s *Store) AddLocalHistory(entry domain.HistoryEntry) error {
	if err := domain.ValidateHistory(entry); err != nil {
		return err
	}
	data, err := encodeLocalMetadata(entry)
	if err != nil {
		return err
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	_, err = s.db.Exec("INSERT INTO local_history(entry_id,played_at,metadata,search_text) VALUES(?,?,?,?) ON CONFLICT(entry_id) DO NOTHING", entry.HistoryEntryID, entry.PlayedAt, data, trackSearchText(entry.Track))
	return err
}

// ListLocalHistory returns newest first, using descending entry ID to break ties.
// Limits outside 1..200 or offsets outside nonnegative JS-safe integers are rejected.
func (s *Store) ListLocalHistory(limit, offset int) ([]domain.HistoryEntry, error) {
	if limit < 1 || limit > maxHistoryPageSize || offset < 0 || int64(offset) > 9007199254740991 {
		return nil, errors.New("INVALID_HISTORY_PAGINATION")
	}
	records, err := s.readLibrary(context.Background(), historyTable, " ORDER BY played_at DESC,entry_id DESC", nil, limit, offset)
	if err != nil {
		return nil, err
	}
	return historyRecords(records), nil
}

// ListLocalHistoryBefore is unaffected by insertions newer than its cursor.
func (s *Store) ListLocalHistoryBefore(limit int, beforePlayedAt int64, beforeEntryID string) (domain.HistoryPage, error) {
	if err := domain.ValidateHistoryPagination(limit, beforePlayedAt, beforeEntryID); err != nil {
		return domain.HistoryPage{}, err
	}
	suffix := ""
	args := []any{}
	if beforePlayedAt != 0 {
		suffix = " WHERE (played_at,entry_id) < (?,?)"
		args = append(args, beforePlayedAt, beforeEntryID)
	}
	suffix += " ORDER BY played_at DESC,entry_id DESC"
	records, err := s.readLibrary(context.Background(), historyTable, suffix, args, limit+1, 0)
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

func historyRecords(records []libraryRecord) []domain.HistoryEntry {
	entries := []domain.HistoryEntry{}
	for _, r := range records {
		entries = append(entries, r.history)
	}
	return entries
}

func (s *Store) ClearLocalHistory() error {
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM local_history"); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM local_record_quarantine WHERE source_table='local_history'"); err != nil {
		return err
	}
	return tx.Commit()
}
