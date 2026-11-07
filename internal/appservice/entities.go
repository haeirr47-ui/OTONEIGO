package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"strings"
	"time"
)

// EntityCatalog is optional so existing catalog implementations remain valid.
type EntityCatalog interface {
	SearchEntities(context.Context, string, string, int) (domain.EntitySearchResult, error)
	GetEntityDetail(context.Context, string, string) (domain.EntityDetail, error)
}

func (s *AppService) SearchEntities(ctx context.Context, query, kind string, page int) (domain.EntitySearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 512 || page < 0 || page > 1000 || !domain.ValidEntitySearchKind(kind) {
		return domain.EntitySearchResult{}, errors.New("INVALID_ENTITY_REQUEST")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client, ok := s.client.(EntityCatalog)
	if !ok {
		return domain.EntitySearchResult{}, errors.New("DISCOVERY_UNAVAILABLE")
	}
	result, err := client.SearchEntities(ctx, query, kind, page)
	if ctx.Err() != nil {
		return domain.EntitySearchResult{}, ctx.Err()
	}
	return result, err
}

func (s *AppService) GetEntityDetail(ctx context.Context, kind, id string) (domain.EntityDetail, error) {
	if !domain.ValidEntityDetailKind(kind) || !domain.ValidID(id) {
		return domain.EntityDetail{}, errors.New("INVALID_ENTITY_REQUEST")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client, ok := s.client.(EntityCatalog)
	if !ok {
		return domain.EntityDetail{}, errors.New("DISCOVERY_UNAVAILABLE")
	}
	result, err := client.GetEntityDetail(ctx, kind, id)
	if ctx.Err() != nil {
		return domain.EntityDetail{}, ctx.Err()
	}
	return result, err
}
