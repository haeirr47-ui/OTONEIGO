package appservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

type resourceFixture struct {
	location string
	err      error
	calls    int
}

func (f *resourceFixture) Cover(context.Context, string) (string, error) {
	f.calls++
	return f.location, f.err
}
func (f *resourceFixture) Stats() domain.ResourceCacheStats {
	return domain.ResourceCacheStats{Available: true}
}
func (f *resourceFixture) Clear() (domain.ResourceCacheClearResult, error) {
	return domain.ResourceCacheClearResult{}, f.err
}

func TestResourceDisplayBoundary(t *testing.T) {
	const ref = "https://p1.music.126.net/cover.jpg"
	for _, location := range []string{ref, "/artwork/" + strings.Repeat("a", 64), "https://invalid.test/private?token=secret", "/artwork/../private", "javascript:alert(1)"} {
		cache := &resourceFixture{location: location}
		s := New(nil, nil, Options{ResourceCache: cache})
		got, err := s.ResolveCover(context.Background(), ref)
		want := ref
		if strings.HasPrefix(location, "/artwork/") && len(location) == 73 {
			want = location
		}
		if err != nil || got != want {
			t.Fatalf("unexpected display result %q, %v", got, err)
		}
		if _, err = s.ResolveCover(context.Background(), "https://invalid.test/image.jpg"); err == nil || cache.calls != 1 {
			t.Fatal("untrusted request reached cache")
		}
	}
	cache := &resourceFixture{err: errors.New("private diagnostic")}
	s := New(nil, nil, Options{ResourceCache: cache})
	if got, err := s.ResolveCover(context.Background(), ref); got != ref || err != nil {
		t.Fatal("cache failure blocked ordinary cover")
	}
	if _, err := s.ClearResourceCache(); err == nil || err.Error() != "RESOURCE_CACHE_UNAVAILABLE" {
		t.Fatal("unsafe cache error")
	}
}

func TestLegacyResourceSettingsNormalizeAndPreserveChoice(t *testing.T) {
	s, db := openLibraryService(t, domain.Capabilities{})
	legacy := domain.DefaultSettings()
	legacy.ResourceCache = domain.ResourceCacheSettings{}
	if err := db.SetSettings(legacy); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSettings()
	if err != nil || got.ResourceCache != domain.DefaultResourceCacheSettings() {
		t.Fatalf("legacy defaults %v %v", got, err)
	}
	got.ResourceCache = domain.ResourceCacheSettings{Enabled: false, MaxBytes: 64 * 1024 * 1024}
	if _, err = s.SetSettings(got); err != nil {
		t.Fatal(err)
	}
	got.ResourceCache = domain.ResourceCacheSettings{}
	got.Volume = .25
	updated, err := s.SetSettings(got)
	if err != nil || updated.ResourceCache.Enabled || updated.ResourceCache.MaxBytes != 64*1024*1024 || updated.Volume != .25 {
		t.Fatalf("older payload lost cache choice %+v %v", updated, err)
	}
}
