package appservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

type ResourceCacheService interface {
	Cover(context.Context, string) (string, error)
	Stats() domain.ResourceCacheStats
	Clear() (domain.ResourceCacheClearResult, error)
}

// ResolveCover returns a runtime display location. It never changes the remote
// cover reference in a persisted track snapshot.
func (s *AppService) ResolveCover(ctx context.Context, ref string) (string, error) {
	if !domain.ValidCover(ref) {
		return "", errors.New("INVALID_COVER_REFERENCE")
	}
	if ref == "" || s.options.ResourceCache == nil {
		return ref, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	location, err := s.options.ResourceCache.Cover(ctx, ref)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		// A disposable cache failure must not block the ordinary trusted image.
		return ref, nil
	}
	if location != ref && !validArtworkLocation(location) {
		return ref, nil
	}
	return location, nil
}

func validArtworkLocation(location string) bool {
	const prefix = "/artwork/"
	if !strings.HasPrefix(location, prefix) || len(location) != len(prefix)+64 {
		return false
	}
	for _, c := range location[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *AppService) GetResourceCacheStats() domain.ResourceCacheStats {
	if s.options.ResourceCache == nil {
		return domain.ResourceCacheStats{ErrorCode: "RESOURCE_CACHE_UNAVAILABLE"}
	}
	return s.options.ResourceCache.Stats()
}

func (s *AppService) ClearResourceCache() (domain.ResourceCacheClearResult, error) {
	if s.options.ResourceCache == nil {
		return domain.ResourceCacheClearResult{}, errors.New("RESOURCE_CACHE_UNAVAILABLE")
	}
	result, err := s.options.ResourceCache.Clear()
	if err != nil {
		return result, errors.New("RESOURCE_CACHE_UNAVAILABLE")
	}
	return result, nil
}
