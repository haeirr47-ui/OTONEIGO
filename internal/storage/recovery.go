package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/voici5986/otoneigo/internal/domain"
)

// RecoverySummary contains counts only. Original records never cross the storage boundary.
type RecoverySummary struct {
	QuarantinedFavorites int64
	QuarantinedHistory   int64
	SettingsRecovered    bool
}

func (s *Store) RecoverySummary() (RecoverySummary, error) {
	var result RecoverySummary
	err := s.db.QueryRow(`SELECT COALESCE(SUM(source_table='local_favorites'),0), COALESCE(SUM(source_table='local_history'),0), COALESCE(MAX(source_table='local_settings'),0) FROM local_record_quarantine`).Scan(&result.QuarantinedFavorites, &result.QuarantinedHistory, &result.SettingsRecovered)
	return result, err
}

func createRecordQuarantine(tx *sql.Tx) error {
	// The original-value columns deliberately have no SQLite affinity: NULL,
	// BLOB, and invalid scalar types must survive byte-for-byte/type-for-type.
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS local_record_quarantine (
  quarantine_id INTEGER PRIMARY KEY, source_table TEXT NOT NULL,
  original_rowid INTEGER NOT NULL, reason TEXT NOT NULL,
  original_id, original_metadata, original_value, original_search_text
 )`)
	return err
}

type libraryTable struct{ name, id, value, reason string }

var favoriteTable = libraryTable{"local_favorites", "song_id", "added_order", "INVALID_STORED_FAVORITE"}
var historyTable = libraryTable{"local_history", "entry_id", "played_at", "INVALID_STORED_HISTORY"}

// Copy and removal are part of the caller's transaction. SQL copies original
// values directly, including oversized data never materialized in Go.
func quarantineRecord(tx *sql.Tx, table libraryTable, rowID int64, hasSearch bool) error {
	search := "NULL"
	if hasSearch {
		search = "search_text"
	}
	value := table.value
	if value == "" {
		value = "NULL"
	}
	query := `INSERT INTO local_record_quarantine(source_table,original_rowid,reason,original_id,original_metadata,original_value,original_search_text) SELECT ?,rowid,?,` + table.id + `,metadata,` + value + `,` + search + ` FROM ` + table.name + ` WHERE rowid=?`
	if _, err := tx.Exec(query, table.name, table.reason, rowID); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM "+table.name+" WHERE rowid=?", rowID)
	return err
}

func recoverSettings(tx *sql.Tx) error {
	var raw any
	err := tx.QueryRow(`SELECT CASE WHEN typeof(metadata)='text' AND length(CAST(metadata AS BLOB))<=131072 THEN metadata ELSE NULL END FROM local_settings WHERE id=1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	data, text := raw.(string)
	var settings domain.LocalSettings
	if text && decodeLocalMetadata(data, &settings) == nil && domain.ValidateSettings(settings) == nil {
		return nil
	}
	if _, err = tx.Exec(`INSERT INTO local_record_quarantine(source_table,original_rowid,reason,original_id,original_metadata) SELECT 'local_settings',rowid,'INVALID_STORED_SETTINGS',id,metadata FROM local_settings WHERE id=1`); err != nil {
		return err
	}
	data, err = encodeLocalMetadata(domain.DefaultSettings())
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE local_settings SET metadata=? WHERE id=1", data)
	return err
}

type libraryRecord struct {
	rowID   int64
	id      string
	value   int64
	track   domain.TrackSnapshot
	history domain.HistoryEntry
}

func libraryColumns(table libraryTable) string {
	value := table.value
	if value == "" {
		value = "1" // Legacy favorites have no ordering column yet.
	} else {
		value = "CASE WHEN typeof(" + value + ")='integer' THEN " + value + " ELSE NULL END"
	}
	return "rowid,CASE WHEN typeof(" + table.id + ")='text' AND length(CAST(" + table.id + " AS BLOB))<=1024 THEN " + table.id + " ELSE NULL END," + value + ",CASE WHEN typeof(metadata)='text' AND length(CAST(metadata AS BLOB))<=131072 THEN metadata ELSE NULL END"
}

func scanLibraryRecord(rows *sql.Rows, table libraryTable) (libraryRecord, bool, error) {
	var r libraryRecord
	var id, value, raw any
	if err := rows.Scan(&r.rowID, &id, &value, &raw); err != nil {
		return r, false, err
	}
	var idOK, valueOK, dataOK bool
	r.id, idOK = id.(string)
	r.value, valueOK = value.(int64)
	data, dataOK := raw.(string)
	if !idOK || !valueOK || !dataOK || !utf8.ValidString(r.id) {
		return r, false, nil
	}
	if table.name == historyTable.name {
		if decodeLocalMetadata(data, &r.history) != nil || domain.ValidateHistory(r.history) != nil || r.history.HistoryEntryID != r.id || r.history.PlayedAt != r.value {
			return r, false, nil
		}
		r.track = r.history.Track
	} else {
		if decodeLocalMetadata(data, &r.track) != nil || domain.ValidateTrack(r.track) != nil || r.track.NeteaseSongID != r.id || r.value < 1 || r.value > 9007199254740991 {
			return r, false, nil
		}
	}
	return r, true, nil
}

// Only pre-seven databases receive this one-time bounded migration scan.
func recoverLibrary(tx *sql.Tx, table libraryTable, hasSearch bool) error {
	var after int64
	first := true
	for {
		query := "SELECT " + libraryColumns(table) + " FROM " + table.name
		args := []any{}
		if !first {
			query += " WHERE rowid>?"
			args = append(args, after)
		}
		query += " ORDER BY rowid LIMIT 100"
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		count := 0
		bad := []int64{}
		for rows.Next() {
			r, valid, e := scanLibraryRecord(rows, table)
			if e != nil {
				rows.Close()
				return e
			}
			count++
			after = r.rowID
			if !valid {
				bad = append(bad, r.rowID)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range bad {
			if err = quarantineRecord(tx, table, id, hasSearch); err != nil {
				return err
			}
		}
		if count < 100 {
			return nil
		}
		first = false
	}
}

// Read-time recovery visits only requested query pages. A transaction keeps
// pagination stable while bad rows are preserved and removed, and batches close
// their cursor before any writes on the single SQLite connection.
func (s *Store) readLibrary(ctx context.Context, table libraryTable, suffix string, args []any, limit, offset int) ([]libraryRecord, error) {
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := []libraryRecord{}
	for {
		batchLimit := 100
		if limit > 0 && limit-len(result) < batchLimit {
			batchLimit = limit - len(result)
		}
		queryArgs := append(append([]any{}, args...), batchLimit, offset+len(result))
		rows, e := tx.QueryContext(ctx, "SELECT "+libraryColumns(table)+" FROM "+table.name+suffix+" LIMIT ? OFFSET ?", queryArgs...)
		if e != nil {
			return nil, e
		}
		bad := []int64{}
		count := 0
		for rows.Next() {
			r, valid, e := scanLibraryRecord(rows, table)
			if e != nil {
				rows.Close()
				return nil, e
			}
			count++
			if valid {
				result = append(result, r)
			} else {
				bad = append(bad, r.rowID)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, id := range bad {
			if err = quarantineRecord(tx, table, id, true); err != nil {
				return nil, err
			}
		}
		if count < batchLimit || (limit > 0 && len(result) >= limit) {
			break
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Settings contain objects and scalars only. Overlay known fields recursively,
// preserving every unknown member while replacing all known settings values.
func mergeSettingsMetadata(original string, settings domain.LocalSettings) (string, error) {
	var old map[string]json.RawMessage
	if err := json.Unmarshal([]byte(original), &old); err != nil {
		return "", errors.New("INVALID_STORED_SETTINGS")
	}
	encoded, err := encodeLocalMetadata(settings)
	if err != nil {
		return "", err
	}
	var known map[string]json.RawMessage
	if err = json.Unmarshal([]byte(encoded), &known); err != nil {
		return "", err
	}
	merged, err := overlayMetadataObject(old, known)
	if err != nil {
		return "", err
	}
	if len(merged) > maxLibraryMetadataBytes {
		return "", errors.New("LOCAL_METADATA_TOO_LARGE")
	}
	return string(merged), nil
}

// encoding/json matches tagged struct fields case-insensitively. Remove every
// recognized alias before writing canonical keys, including nested objects. Merge
// unknown members from aliased objects so forward-compatible extensions survive.
func overlayMetadataObject(old, known map[string]json.RawMessage) ([]byte, error) {
	for key, value := range known {
		var next map[string]json.RawMessage
		if json.Unmarshal(value, &next) == nil && next != nil {
			previous := map[string]json.RawMessage{}
			aliases := []string{}
			for alias := range old {
				if strings.EqualFold(alias, key) {
					aliases = append(aliases, alias)
				}
			}
			sort.Strings(aliases)
			for _, alias := range aliases {
				var object map[string]json.RawMessage
				if json.Unmarshal(old[alias], &object) == nil {
					if err := mergeAliasedMetadataObjects(previous, object, next); err != nil {
						return nil, err
					}
				}
				delete(old, alias)
			}
			merged, err := overlayMetadataObject(previous, next)
			if err != nil {
				return nil, err
			}
			old[key] = merged
		} else {
			for alias := range old {
				if strings.EqualFold(alias, key) {
					delete(old, alias)
				}
			}
			old[key] = value
		}
	}
	return json.Marshal(old)
}

// Merge original objects before replacing known typed values. Unknown leaves
// are opaque: conflicting values cannot be discarded by choosing an alias.
func mergeAliasedMetadataObjects(target, source, known map[string]json.RawMessage) error {
	for key, raw := range source {
		previous, exists := target[key]
		if !exists {
			target[key] = raw
			continue
		}
		var schema json.RawMessage
		recognized := false
		for field, value := range known {
			if strings.EqualFold(field, key) {
				schema = value
				recognized = true
				break
			}
		}
		var oldObject, newObject, knownObject map[string]json.RawMessage
		oldIsObject := json.Unmarshal(previous, &oldObject) == nil && oldObject != nil
		newIsObject := json.Unmarshal(raw, &newObject) == nil && newObject != nil
		if recognized {
			_ = json.Unmarshal(schema, &knownObject)
		}
		if oldIsObject && newIsObject {
			if err := mergeAliasedMetadataObjects(oldObject, newObject, knownObject); err != nil {
				return err
			}
			merged, err := json.Marshal(oldObject)
			if err != nil {
				return err
			}
			target[key] = merged
		} else if recognized {
			// Known object values will be overlaid later. Retain any old object so
			// unknown descendants survive a null/scalar alias of that known object.
			if knownObject != nil && oldIsObject {
				continue
			}
			target[key] = raw
		} else {
			var oldCompact, newCompact bytes.Buffer
			if json.Compact(&oldCompact, previous) != nil || json.Compact(&newCompact, raw) != nil || !bytes.Equal(oldCompact.Bytes(), newCompact.Bytes()) {
				return errors.New("LOCAL_METADATA_REQUIRES_NEWER_VERSION")
			}
		}
	}
	return nil
}
