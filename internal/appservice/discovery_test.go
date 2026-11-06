package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
	"time"
)

type discoveryFake struct {
	lateCatalog
	toplists func(context.Context) (domain.ToplistResult, error)
	playlist func(context.Context, string, int) (domain.PublicPlaylistPage, error)
}

func (f discoveryFake) GetToplists(ctx context.Context) (domain.ToplistResult, error) {
	return f.toplists(ctx)
}
func (f discoveryFake) GetPublicPlaylist(ctx context.Context, id string, offset int) (domain.PublicPlaylistPage, error) {
	return f.playlist(ctx, id, offset)
}
func TestDiscoveryOptionalCatalogAndInputBounds(t *testing.T) {
	service := New(lateCatalog{}, nil)
	if _, err := service.GetToplists(context.Background()); err == nil || err.Error() != "DISCOVERY_UNAVAILABLE" {
		t.Fatal(err)
	}
	if _, err := service.GetPublicPlaylist(context.Background(), "1", 0); err == nil || err.Error() != "DISCOVERY_UNAVAILABLE" {
		t.Fatal(err)
	}
	if _, err := service.GetPublicPlaylist(context.Background(), "1", 1); err == nil || err.Error() != "INVALID_PLAYLIST_OFFSET" {
		t.Fatal(err)
	}
}
func TestDiscoveryDeadlinePropagationAndLateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := discoveryFake{
		toplists: func(ctx context.Context) (domain.ToplistResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 12*time.Second {
				t.Fatal("missing finite deadline")
			}
			cancel()
			return domain.ToplistResult{Items: []domain.Toplist{{PlaylistID: "1"}}}, nil
		},
		playlist: func(ctx context.Context, id string, offset int) (domain.PublicPlaylistPage, error) {
			if id != "9007199254740993" || offset != 30 {
				t.Fatal("request modified")
			}
			<-ctx.Done()
			return domain.PublicPlaylistPage{PlaylistID: id}, nil
		},
	}
	if result, err := New(f, nil).GetToplists(ctx); !errors.Is(err, context.Canceled) || len(result.Items) != 0 {
		t.Fatal(result, err)
	}
	short, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if result, err := New(f, nil).GetPublicPlaylist(short, "9007199254740993", 30); !errors.Is(err, context.DeadlineExceeded) || result.PlaylistID != "" {
		t.Fatal(result, err)
	}
}
