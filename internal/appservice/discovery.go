package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"time"
)

// DiscoveryCatalog is optional so existing catalog implementations remain valid.
type DiscoveryCatalog interface {
	GetToplists(context.Context) (domain.ToplistResult, error)
	GetPublicPlaylist(context.Context, string, int) (domain.PublicPlaylistPage, error)
}

func (s *AppService) GetToplists(ctx context.Context) (domain.ToplistResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client, ok := s.client.(DiscoveryCatalog)
	if !ok {
		return domain.ToplistResult{}, errors.New("DISCOVERY_UNAVAILABLE")
	}
	result, err := client.GetToplists(ctx)
	if ctx.Err() != nil {
		return domain.ToplistResult{}, ctx.Err()
	}
	return result, err
}

func (s *AppService) GetPublicPlaylist(ctx context.Context, id string, offset int) (domain.PublicPlaylistPage, error) {
	if err := domain.ValidatePublicPlaylistRequest(id, offset); err != nil {
		return domain.PublicPlaylistPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client, ok := s.client.(DiscoveryCatalog)
	if !ok {
		return domain.PublicPlaylistPage{}, errors.New("DISCOVERY_UNAVAILABLE")
	}
	result, err := client.GetPublicPlaylist(ctx, id, offset)
	if ctx.Err() != nil {
		return domain.PublicPlaylistPage{}, ctx.Err()
	}
	return result, err
}
