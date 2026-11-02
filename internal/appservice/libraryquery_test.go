package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
)

func TestLocalQueryServiceContracts(t *testing.T) {
	s, store := openLibraryService(t, domain.Capabilities{})
	ctx := context.Background()
	if err := s.SetLocalFavorite(fixtureTrack("123", "Japanese"), true); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "event", PlayedAt: 5, Track: fixtureTrack("123", "Japanese")}); err != nil {
		t.Fatal(err)
	}
	p, err := s.SearchLocalFavorites(ctx, "japan", 1, 0, "")
	if err != nil || len(p.Tracks) != 1 || p.NextSongID != "123" {
		t.Fatal(p, err)
	}
	h, err := s.SearchLocalHistory(ctx, "japan", 1, 0, "")
	if err != nil || len(h.Entries) != 1 {
		t.Fatal(h, err)
	}
	ids, err := s.GetLocalFavoriteIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "123" {
		t.Fatal(ids, err)
	}
	if err = s.RecordSearchQuery(ctx, " Query "); err != nil {
		t.Fatal(err)
	}
	history, err := s.ListSearchHistory(ctx, 10)
	if err != nil || len(history) != 1 || history[0].Query != "Query" {
		t.Fatal(history, err)
	}
	if err = s.DeleteSearchQuery(ctx, "query"); err != nil {
		t.Fatal(err)
	}
	if err = s.ClearSearchHistory(ctx); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.SearchLocalFavorites(canceled, "", 1, 0, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = s.RecordSearchQuery(canceled, "query"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = s.SearchLocalFavorites(ctx, "", 201, 0, ""); err == nil || err.Error() != "INVALID_FAVORITE_PAGINATION" {
		t.Fatal(err)
	}
	store.Close()
	if _, err = s.SearchLocalFavorites(ctx, "", 1, 0, ""); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatal(err)
	}
	if err = s.RecordSearchQuery(ctx, "query"); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatal(err)
	}
}
func TestLocalQueryOptionalLegacyCompatibility(t *testing.T) {
	ctx := context.Background()
	for _, lib := range []LocalLibrary{nil, failingLibrary{}} {
		s := New(nil, nil, Options{Library: lib})
		if _, err := s.SearchLocalFavorites(ctx, "", 1, 0, ""); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
		if _, err := s.SearchLocalHistory(ctx, "", 1, 0, ""); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
		if _, err := s.GetLocalFavoriteIDs(ctx); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
		if _, err := s.ListSearchHistory(ctx, 10); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
		if err := s.RecordSearchQuery(ctx, "query"); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
		if err := s.DeleteSearchQuery(ctx, "query"); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
		if err := s.ClearSearchHistory(ctx); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
			t.Fatal(err)
		}
	}
}
