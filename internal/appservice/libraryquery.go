package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
)

// LocalQueryLibrary extends optional query capabilities without changing legacy libraries.
type LocalQueryLibrary interface {
	SearchLocalFavorites(context.Context, string, int, int64, string) (domain.FavoritePage, error)
	SearchLocalHistory(context.Context, string, int, int64, string) (domain.HistoryPage, error)
	GetLocalFavoriteIDs(context.Context) ([]string, error)
	ListSearchHistory(context.Context, int) ([]domain.SearchHistoryEntry, error)
	RecordSearchQuery(context.Context, string) error
	DeleteSearchQuery(context.Context, string) error
	ClearSearchHistory(context.Context) error
}

func (s *AppService) queryLibrary() (LocalQueryLibrary, error) {
	lib, ok := s.options.Library.(LocalQueryLibrary)
	if !ok {
		return nil, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	return lib, nil
}
func queryStorageError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return storageError(err)
}
func (s *AppService) SearchLocalFavorites(ctx context.Context, query string, limit int, beforeAddedOrder int64, beforeSongID string) (domain.FavoritePage, error) {
	lib, err := s.queryLibrary()
	if err != nil {
		return domain.FavoritePage{}, err
	}
	v, err := lib.SearchLocalFavorites(ctx, query, limit, beforeAddedOrder, beforeSongID)
	return v, queryStorageError(ctx, err)
}
func (s *AppService) SearchLocalHistory(ctx context.Context, query string, limit int, beforePlayedAt int64, beforeEntryID string) (domain.HistoryPage, error) {
	lib, err := s.queryLibrary()
	if err != nil {
		return domain.HistoryPage{}, err
	}
	v, err := lib.SearchLocalHistory(ctx, query, limit, beforePlayedAt, beforeEntryID)
	return v, queryStorageError(ctx, err)
}
func (s *AppService) GetLocalFavoriteIDs(ctx context.Context) ([]string, error) {
	lib, err := s.queryLibrary()
	if err != nil {
		return nil, err
	}
	v, err := lib.GetLocalFavoriteIDs(ctx)
	return v, queryStorageError(ctx, err)
}
func (s *AppService) ListSearchHistory(ctx context.Context, limit int) ([]domain.SearchHistoryEntry, error) {
	lib, err := s.queryLibrary()
	if err != nil {
		return nil, err
	}
	v, err := lib.ListSearchHistory(ctx, limit)
	return v, queryStorageError(ctx, err)
}
func (s *AppService) RecordSearchQuery(ctx context.Context, query string) error {
	lib, err := s.queryLibrary()
	if err != nil {
		return err
	}
	return queryStorageError(ctx, lib.RecordSearchQuery(ctx, query))
}
func (s *AppService) DeleteSearchQuery(ctx context.Context, query string) error {
	lib, err := s.queryLibrary()
	if err != nil {
		return err
	}
	return queryStorageError(ctx, lib.DeleteSearchQuery(ctx, query))
}
func (s *AppService) ClearSearchHistory(ctx context.Context) error {
	lib, err := s.queryLibrary()
	if err != nil {
		return err
	}
	return queryStorageError(ctx, lib.ClearSearchHistory(ctx))
}
