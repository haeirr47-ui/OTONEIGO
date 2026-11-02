package appservice

import (
	"errors"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/storage"
)

func TestPlaylistBindingsPersistAndPreserveSafeErrors(t *testing.T) {
	_, store := openLibraryService(t, domain.Capabilities{})
	s := New(nil, nil, Options{Playlists: store})
	p, err := s.CreateLocalPlaylist("  offline  ")
	if err != nil || p.Name != "offline" {
		t.Fatal(p, err)
	}
	update := domain.PlaylistUpdate{PlaylistID: p.PlaylistID, ExpectedRevision: 1, Name: "new name", Tracks: []domain.TrackSnapshot{fixtureTrack("9007199254740993", "song")}}
	p, err = s.UpdateLocalPlaylist(update)
	if err != nil || p.Revision != 2 {
		t.Fatal(p, err)
	}
	if _, err = s.UpdateLocalPlaylist(update); !errors.Is(err, storage.ErrStalePlaylistRevision) {
		t.Fatal("lost CAS error", err)
	}
	if got, e := s.GetLocalPlaylist(p.PlaylistID); e != nil || got.Tracks[0].NeteaseSongID != "9007199254740993" {
		t.Fatal(got, e)
	}
	if got, e := s.ListLocalPlaylists(); e != nil || len(got) != 1 || got[0].TrackCount != 1 {
		t.Fatal(got, e)
	}
	if err = s.DeleteLocalPlaylist(p.PlaylistID, p.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetLocalPlaylist(p.PlaylistID); err == nil || err.Error() != "PLAYLIST_NOT_FOUND" {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateLocalPlaylist("valid"); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatal("internal error leaked", err)
	}
}

// Invalid binding arguments are rejected even before a persistence provider is consulted.
func TestPlaylistBindingsValidateUntrustedInputs(t *testing.T) {
	s := New(nil, nil)
	if _, err := s.GetLocalPlaylist("../invalid"); err == nil || err.Error() != "INVALID_PLAYLIST_ID" {
		t.Fatal(err)
	}
	if _, err := s.CreateLocalPlaylist("\x00"); err == nil || err.Error() != "INVALID_PLAYLIST_NAME" {
		t.Fatal(err)
	}
	track := fixtureTrack("123", "song")
	if _, err := s.UpdateLocalPlaylist(domain.PlaylistUpdate{PlaylistID: "valid", ExpectedRevision: 1, Name: "valid", Tracks: []domain.TrackSnapshot{track, track}}); err == nil || err.Error() != "INVALID_PLAYLIST_TRACKS" {
		t.Fatal(err)
	}
	if err := s.DeleteLocalPlaylist("valid", domain.MaxPlaylistInteger+1); err == nil || err.Error() != "INVALID_PLAYLIST_REVISION" {
		t.Fatal(err)
	}
	if _, err := s.ListLocalPlaylists(); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatal(err)
	}
}
