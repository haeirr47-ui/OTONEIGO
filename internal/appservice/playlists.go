package appservice

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
)

type PlaylistLibrary interface {
	ListLocalPlaylists() ([]domain.LocalPlaylistSummary, error)
	GetLocalPlaylist(string) (domain.LocalPlaylist, error)
	CreateLocalPlaylist(string) (domain.LocalPlaylist, error)
	UpdateLocalPlaylist(domain.PlaylistUpdate) (domain.LocalPlaylist, error)
	DeleteLocalPlaylist(string, int64) error
}

func (s *AppService) ListLocalPlaylists() ([]domain.LocalPlaylistSummary, error) {
	if s.options.Playlists == nil {
		return nil, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Playlists.ListLocalPlaylists()
	return v, storageError(err)
}

func (s *AppService) GetLocalPlaylist(id string) (domain.LocalPlaylist, error) {
	if err := domain.ValidatePlaylistID(id); err != nil {
		return domain.LocalPlaylist{}, err
	}
	if s.options.Playlists == nil {
		return domain.LocalPlaylist{}, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Playlists.GetLocalPlaylist(id)
	return v, storageError(err)
}

func (s *AppService) CreateLocalPlaylist(name string) (domain.LocalPlaylist, error) {
	name, err := domain.NormalizePlaylistName(name)
	if err != nil {
		return domain.LocalPlaylist{}, err
	}
	if s.options.Playlists == nil {
		return domain.LocalPlaylist{}, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Playlists.CreateLocalPlaylist(name)
	return v, storageError(err)
}

func (s *AppService) UpdateLocalPlaylist(update domain.PlaylistUpdate) (domain.LocalPlaylist, error) {
	if err := domain.ValidatePlaylistUpdate(update); err != nil {
		return domain.LocalPlaylist{}, err
	}
	update.Name, _ = domain.NormalizePlaylistName(update.Name)
	if s.options.Playlists == nil {
		return domain.LocalPlaylist{}, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Playlists.UpdateLocalPlaylist(update)
	return v, storageError(err)
}

func (s *AppService) DeleteLocalPlaylist(id string, expectedRevision int64) error {
	if err := domain.ValidatePlaylistID(id); err != nil {
		return err
	}
	if err := domain.ValidatePlaylistRevision(expectedRevision); err != nil {
		return err
	}
	if s.options.Playlists == nil {
		return errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	return storageError(s.options.Playlists.DeleteLocalPlaylist(id, expectedRevision))
}
