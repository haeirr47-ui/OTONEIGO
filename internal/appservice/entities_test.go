package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
	"time"
)

type entityFake struct {
	lateCatalog
	search func(context.Context, string, string, int) (domain.EntitySearchResult, error)
	detail func(context.Context, string, string) (domain.EntityDetail, error)
}

func (f entityFake) SearchEntities(ctx context.Context, query, kind string, page int) (domain.EntitySearchResult, error) {
	return f.search(ctx, query, kind, page)
}
func (f entityFake) GetEntityDetail(ctx context.Context, kind, id string) (domain.EntityDetail, error) {
	return f.detail(ctx, kind, id)
}

func TestEntityOptionalCatalogAndInputBounds(t *testing.T) {
	service := New(lateCatalog{}, nil)
	if _, err := service.SearchEntities(context.Background(), "查询", "album", 0); err == nil || err.Error() != "DISCOVERY_UNAVAILABLE" {
		t.Fatal(err)
	}
	if _, err := service.GetEntityDetail(context.Background(), "album", "1"); err == nil || err.Error() != "DISCOVERY_UNAVAILABLE" {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query, kind string
		page        int
	}{
		{"", "album", 0}, {"a", "song", 0}, {"a", "album", -1}, {"a", "album", 1001},
	} {
		if _, err := service.SearchEntities(context.Background(), tc.query, tc.kind, tc.page); err == nil || err.Error() != "INVALID_ENTITY_REQUEST" {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	for _, tc := range []struct{ kind, id string }{{"playlist", "1"}, {"album", "0"}, {"", "1"}} {
		if _, err := service.GetEntityDetail(context.Background(), tc.kind, tc.id); err == nil || err.Error() != "INVALID_ENTITY_REQUEST" {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

func TestEntityDeadlinePropagationAndLateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := entityFake{
		search: func(ctx context.Context, query, kind string, page int) (domain.EntitySearchResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 12*time.Second {
				t.Fatal("missing finite deadline")
			}
			if query != "查询" || kind != domain.EntityAlbum || page != 2 {
				t.Fatal("request modified")
			}
			cancel()
			return domain.EntitySearchResult{Items: []domain.EntitySummary{{ID: "1"}}}, nil
		},
		detail: func(ctx context.Context, kind, id string) (domain.EntityDetail, error) {
			if kind != domain.EntityArtist || id != "9007199254740993" {
				t.Fatal("request modified")
			}
			<-ctx.Done()
			return domain.EntityDetail{ID: id}, nil
		},
	}
	if result, err := New(f, nil).SearchEntities(ctx, "查询", domain.EntityAlbum, 2); !errors.Is(err, context.Canceled) || len(result.Items) != 0 {
		t.Fatal(result, err)
	}
	short, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if result, err := New(f, nil).GetEntityDetail(short, domain.EntityArtist, "9007199254740993"); !errors.Is(err, context.DeadlineExceeded) || result.ID != "" {
		t.Fatal(result, err)
	}
}
