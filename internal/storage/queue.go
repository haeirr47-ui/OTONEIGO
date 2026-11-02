package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/voici5986/otoneigo/internal/domain"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
)

var ErrStaleRevision = errors.New("STALE_QUEUE_REVISION")

type Store struct {
	db     *sql.DB
	writer sync.Mutex
}

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var active *sql.Tx
	fail := func(e error) (*Store, error) {
		if active != nil {
			active.Rollback()
		}
		db.Close()
		return nil, e
	}
	for _, s := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err = db.Exec(s); err != nil {
			return fail(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return fail(err)
	}
	active = tx
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > 7 {
		return fail(errors.New("UNSUPPORTED_SCHEMA_VERSION"))
	}
	if version == 0 {
		for _, s := range []string{"CREATE TABLE queue_snapshot (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, metadata TEXT NOT NULL)", "PRAGMA user_version=1"} {
			if _, err = tx.Exec(s); err != nil {
				return fail(err)
			}
		}
	}
	if version < 2 {
		for _, statement := range []string{
			"CREATE TABLE local_settings (id INTEGER PRIMARY KEY CHECK(id=1), metadata TEXT NOT NULL)",
			"CREATE TABLE local_favorites (song_id TEXT PRIMARY KEY, metadata TEXT NOT NULL)",
			"CREATE TABLE local_history (entry_id TEXT PRIMARY KEY, played_at INTEGER NOT NULL, metadata TEXT NOT NULL)",
			"CREATE INDEX local_history_order ON local_history(played_at DESC, entry_id DESC)",
			"PRAGMA user_version=2",
		} {
			if _, err = tx.Exec(statement); err != nil {
				return fail(err)
			}
		}
	}
	if version < 3 {
		for _, statement := range []string{
			"CREATE TABLE local_playlists (playlist_id TEXT PRIMARY KEY, name TEXT NOT NULL, revision INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, track_count INTEGER NOT NULL, metadata TEXT NOT NULL)",
			"CREATE INDEX local_playlists_order ON local_playlists(updated_at DESC, playlist_id)",
			"PRAGMA user_version=3",
		} {
			if _, err = tx.Exec(statement); err != nil {
				return fail(err)
			}
		}
	}
	if err = createRecordQuarantine(tx); err != nil {
		return fail(err)
	}
	if err = recoverSettings(tx); err != nil {
		return fail(err)
	}
	if version < 4 {
		legacyFavorites := favoriteTable
		legacyFavorites.value = ""
		if err = recoverLibrary(tx, legacyFavorites, false); err != nil {
			return fail(err)
		}
		for _, statement := range []string{
			"ALTER TABLE local_favorites ADD COLUMN added_order INTEGER NOT NULL DEFAULT 0",
			"WITH ordered AS MATERIALIZED (SELECT song_id, ROW_NUMBER() OVER (ORDER BY song_id DESC) AS position FROM local_favorites) UPDATE local_favorites SET added_order=ordered.position FROM ordered WHERE local_favorites.song_id=ordered.song_id",
			"CREATE INDEX local_favorites_order ON local_favorites(added_order DESC, song_id)",
			"PRAGMA user_version=4",
		} {
			if _, err = tx.Exec(statement); err != nil {
				return fail(err)
			}
		}
	}
	if version < 7 {
		for _, table := range []libraryTable{favoriteTable, historyTable} {
			if err = recoverLibrary(tx, table, version >= 5); err != nil {
				return fail(err)
			}
		}
	}
	if version < 5 {
		if err = migrateLibraryQuery(tx); err != nil {
			return fail(err)
		}
	}
	if version < 6 {
		if err = migrateDesktopSettings(tx); err != nil {
			return fail(err)
		}
	}
	if _, err = tx.Exec("PRAGMA user_version=7"); err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) LoadQueue() (*domain.QueueSnapshot, error) {
	var data string
	var size int
	err := s.db.QueryRow("SELECT length(CAST(metadata AS BLOB)) FROM queue_snapshot WHERE id=1").Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if size > 2*1024*1024 {
		return nil, errors.New("INVALID_STORED_QUEUE")
	}
	err = s.db.QueryRow("SELECT metadata FROM queue_snapshot WHERE id=1").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var q domain.QueueSnapshot
	if err = json.Unmarshal([]byte(data), &q); err != nil {
		return nil, errors.New("INVALID_STORED_QUEUE")
	}
	if err = domain.ValidateQueue(q); err != nil {
		return nil, errors.New("INVALID_STORED_QUEUE")
	}
	return &q, nil
}
func (s *Store) SaveQueue(q domain.QueueSnapshot) error {
	if err := domain.ValidateQueue(q); err != nil {
		return err
	}
	data, err := json.Marshal(q)
	if err != nil {
		return err
	}
	if len(data) > 2*1024*1024 {
		return errors.New("QUEUE_TOO_LARGE")
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	result, err := s.db.Exec("INSERT INTO queue_snapshot(id,revision,metadata) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,metadata=excluded.metadata WHERE excluded.revision>queue_snapshot.revision", q.Revision, string(data))
	if err != nil {
		return fmt.Errorf("save queue: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStaleRevision
	}
	return nil
}
