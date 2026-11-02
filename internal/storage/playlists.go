package storage

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

const maxLocalPlaylists = 100
const maxPlaylistMetadataBytes = 2 * 1024 * 1024

var ErrStalePlaylistRevision = errors.New("STALE_PLAYLIST_REVISION")
var errInvalidStoredPlaylist = errors.New("INVALID_STORED_PLAYLIST")

// The CASE bounds allocation before SQLite transfers potentially corrupt metadata.
const playlistColumns = "playlist_id,name,revision,created_at,updated_at,track_count,CASE WHEN length(CAST(metadata AS BLOB))<=2097152 THEN metadata ELSE NULL END"

type playlistScanner interface{ Scan(...any) error }

func readPlaylist(row playlistScanner) (domain.LocalPlaylist, error) {
	var summary domain.LocalPlaylistSummary
	var data sql.NullString
	err := row.Scan(&summary.PlaylistID, &summary.Name, &summary.Revision, &summary.CreatedAt, &summary.UpdatedAt, &summary.TrackCount, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LocalPlaylist{}, errors.New("PLAYLIST_NOT_FOUND")
	}
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	if !data.Valid || domain.ValidatePlaylistSummary(summary) != nil {
		return domain.LocalPlaylist{}, errInvalidStoredPlaylist
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data.String))
	decoder.DisallowUnknownFields()
	var p domain.LocalPlaylist
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF || domain.ValidateLocalPlaylist(p) != nil || p.PlaylistID != summary.PlaylistID || p.Name != summary.Name || p.Revision != summary.Revision || p.CreatedAt != summary.CreatedAt || p.UpdatedAt != summary.UpdatedAt || len(p.Tracks) != summary.TrackCount {
		return domain.LocalPlaylist{}, errInvalidStoredPlaylist
	}
	if p.Tracks == nil {
		p.Tracks = []domain.TrackSnapshot{}
	}
	return p, nil
}

func encodePlaylist(p domain.LocalPlaylist) (string, error) {
	if err := domain.ValidateLocalPlaylist(p); err != nil {
		return "", err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	if len(data) > maxPlaylistMetadataBytes {
		return "", errors.New("PLAYLIST_TOO_LARGE")
	}
	return string(data), nil
}

func (s *Store) ListLocalPlaylists() ([]domain.LocalPlaylistSummary, error) {
	rows, err := s.db.Query("SELECT " + playlistColumns + " FROM local_playlists ORDER BY updated_at DESC,playlist_id LIMIT 101")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.LocalPlaylistSummary{}
	for rows.Next() {
		if len(result) >= maxLocalPlaylists {
			return nil, errInvalidStoredPlaylist
		}
		p, err := readPlaylist(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.LocalPlaylistSummary{PlaylistID: p.PlaylistID, Name: p.Name, Revision: p.Revision, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, TrackCount: len(p.Tracks)})
	}
	return result, rows.Err()
}

func (s *Store) GetLocalPlaylist(id string) (domain.LocalPlaylist, error) {
	if err := domain.ValidatePlaylistID(id); err != nil {
		return domain.LocalPlaylist{}, err
	}
	return readPlaylist(s.db.QueryRow("SELECT "+playlistColumns+" FROM local_playlists WHERE playlist_id=?", id))
}

func (s *Store) CreateLocalPlaylist(name string) (domain.LocalPlaylist, error) {
	name, err := domain.NormalizePlaylistName(name)
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM local_playlists").Scan(&count); err != nil {
		return domain.LocalPlaylist{}, err
	}
	if count >= maxLocalPlaylists {
		return domain.LocalPlaylist{}, errors.New("PLAYLIST_LIMIT_REACHED")
	}
	var randomID [16]byte
	if _, err = rand.Read(randomID[:]); err != nil {
		return domain.LocalPlaylist{}, err
	}
	now := time.Now().UnixMilli()
	p := domain.LocalPlaylist{PlaylistID: hex.EncodeToString(randomID[:]), Name: name, Tracks: []domain.TrackSnapshot{}, Revision: 1, CreatedAt: now, UpdatedAt: now}
	data, err := encodePlaylist(p)
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	if _, err = tx.Exec("INSERT INTO local_playlists(playlist_id,name,revision,created_at,updated_at,track_count,metadata) VALUES(?,?,?,?,?,?,?)", p.PlaylistID, p.Name, p.Revision, p.CreatedAt, p.UpdatedAt, len(p.Tracks), data); err != nil {
		return domain.LocalPlaylist{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.LocalPlaylist{}, err
	}
	return p, nil
}

func (s *Store) UpdateLocalPlaylist(update domain.PlaylistUpdate) (domain.LocalPlaylist, error) {
	if err := domain.ValidatePlaylistUpdate(update); err != nil {
		return domain.LocalPlaylist{}, err
	}
	update.Name, _ = domain.NormalizePlaylistName(update.Name)
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	defer tx.Rollback()
	p, err := readPlaylist(tx.QueryRow("SELECT "+playlistColumns+" FROM local_playlists WHERE playlist_id=?", update.PlaylistID))
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	if p.Revision != update.ExpectedRevision {
		return domain.LocalPlaylist{}, ErrStalePlaylistRevision
	}
	if p.Revision == domain.MaxPlaylistInteger || p.UpdatedAt == domain.MaxPlaylistInteger {
		return domain.LocalPlaylist{}, errors.New("INVALID_PLAYLIST_REVISION")
	}
	p.Name, p.Tracks, p.Revision = update.Name, update.Tracks, p.Revision+1
	if p.Tracks == nil {
		p.Tracks = []domain.TrackSnapshot{}
	}
	now := time.Now().UnixMilli()
	if now <= p.UpdatedAt {
		now = p.UpdatedAt + 1
	}
	p.UpdatedAt = now
	data, err := encodePlaylist(p)
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	result, err := tx.Exec("UPDATE local_playlists SET name=?,revision=?,updated_at=?,track_count=?,metadata=? WHERE playlist_id=? AND revision=?", p.Name, p.Revision, p.UpdatedAt, len(p.Tracks), data, p.PlaylistID, update.ExpectedRevision)
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return domain.LocalPlaylist{}, err
	} else if n != 1 {
		return domain.LocalPlaylist{}, ErrStalePlaylistRevision
	}
	if err = tx.Commit(); err != nil {
		return domain.LocalPlaylist{}, err
	}
	return p, nil
}

func (s *Store) DeleteLocalPlaylist(id string, expectedRevision int64) error {
	if err := domain.ValidatePlaylistID(id); err != nil {
		return err
	}
	if err := domain.ValidatePlaylistRevision(expectedRevision); err != nil {
		return err
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := readPlaylist(tx.QueryRow("SELECT "+playlistColumns+" FROM local_playlists WHERE playlist_id=?", id))
	if err != nil {
		return err
	}
	if p.Revision != expectedRevision {
		return ErrStalePlaylistRevision
	}
	result, err := tx.Exec("DELETE FROM local_playlists WHERE playlist_id=? AND revision=?", id, expectedRevision)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrStalePlaylistRevision
	}
	return tx.Commit()
}
