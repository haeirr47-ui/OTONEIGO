package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
)

// A backend that races completion against cancellation must not make a stale
// resolved URL visible through the bound service after cancellation wins.
type lateCatalog struct{}

func (lateCatalog) Search(context.Context, string, int) (domain.SearchResult, error) {
	return domain.SearchResult{}, nil
}
func (lateCatalog) Lyrics(context.Context, string) (domain.LyricData, error) {
	return domain.LyricData{}, nil
}
func (lateCatalog) Resolve(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
	<-ctx.Done()
	return domain.ResolvedStream{URL: "https://m7.music.126.net/late", Status: "full"}, nil
}
func TestCancelledLateResolveIsDiscarded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e := New(lateCatalog{}, nil).ResolveStream(ctx, "123", "standard")
	if !errors.Is(e, context.Canceled) || r.URL != "" {
		t.Fatal("cancelled result escaped service")
	}
}
