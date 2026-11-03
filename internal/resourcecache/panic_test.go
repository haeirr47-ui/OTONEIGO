package resourcecache

import (
	"context"
	"image"
	"image/color"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

func awaitResource[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("resource work did not settle")
		var zero T
		return zero
	}
}
func closeResource(t *testing.T, m *Manager) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- m.Close() }()
	if err := awaitResource(t, done); err != nil {
		t.Fatal(err)
	}
}
func TestFetchPanicReleasesCoalescedWaitersAndCapacity(t *testing.T) {
	m := manager(t, Options{})
	entered, release := make(chan struct{}), make(chan struct{})
	key := keyFor("lyric", "1")
	fetch := func(context.Context) ([]byte, error) {
		close(entered)
		<-release
		panic("token=private https://secret.invalid")
	}
	owner, waiter := make(chan error, 1), make(chan error, 1)
	go func() { _, err := m.run(context.Background(), key, "lyric", fetch); owner <- err }()
	awaitResource(t, entered)
	ctx := &observedContext{Context: context.Background(), observed: make(chan struct{})}
	go func() { _, err := m.run(ctx, key, "lyric", fetch); waiter <- err }()
	awaitResource(t, ctx.observed)
	close(release)
	for _, ch := range []<-chan error{owner, waiter} {
		if err := awaitResource(t, ch); err != errCache {
			t.Fatalf("non-generic error: %v", err)
		}
	}
	m.mu.Lock()
	settled := len(m.jobs) == 0 && len(m.lyricWorkers) == 0
	m.mu.Unlock()
	if !settled {
		t.Fatal("job or capacity retained")
	}
	v, err := m.Lyrics(context.Background(), "1", func(context.Context, string) (domain.LyricData, error) {
		return domain.LyricData{Original: "next"}, nil
	})
	if err != nil || v.Original != "next" {
		t.Fatal(v, err)
	}
	closeResource(t, m)
}
func TestCoverTransportPanicFallsBackAndRetries(t *testing.T) {
	img := picture(t)
	calls := 0
	m := manager(t, Options{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			panic("cookie=private")
		}
		return response(r, 200, img), nil
	})}})
	if ref, err := m.Cover(context.Background(), testCover); err != nil || ref != testCover {
		t.Fatal(ref, err)
	}
	if ref, err := m.Cover(context.Background(), testCover); err != nil || ref != "/artwork/"+keyFor("cover", testCover) {
		t.Fatal(ref, err)
	}
	closeResource(t, m)
}

var panicCodecOnce sync.Once

func panicImageBytes() []byte {
	panicCodecOnce.Do(func() {
		image.RegisterFormat("resource-panic-test", "RESOURCEPANIC", func(io.Reader) (image.Image, error) { panic("decoder secret") }, func(io.Reader) (image.Config, error) {
			return image.Config{ColorModel: color.RGBAModel, Width: 1, Height: 1}, nil
		})
	})
	return []byte("RESOURCEPANICpayload")
}
func TestDecoderPanicDropsValidationAndReleasesCapacity(t *testing.T) {
	m := manager(t, Options{})
	b := panicImageBytes()
	key := keyFor("cover", testCover)
	e := entry{Key: key, Kind: "cover", Digest: digest(b), Size: int64(len(b)), Created: time.Now().UnixNano(), Access: time.Now().UnixNano()}
	if err := os.WriteFile(m.filename(key), b, 0600); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.entries[key] = e
	m.mu.Unlock()
	done := make(chan []byte, 1)
	go func() {
		m.mu.Lock()
		data := m.getLocked(context.Background(), key, "cover")
		m.mu.Unlock()
		done <- data
	}()
	if data := awaitResource(t, done); data != nil {
		t.Fatal("panic decoder published bytes")
	}
	m.mu.Lock()
	_, exists := m.entries[key]
	settled := len(m.validating) == 0 && len(m.validationWorkers) == 0
	m.mu.Unlock()
	if exists || !settled {
		t.Fatal("invalid entry, validation or capacity retained")
	}
	// The download codec boundary must also release capacity and allow a retry.
	if _, err := m.run(context.Background(), key, "cover", func(context.Context) ([]byte, error) { return b, nil }); err != errCache {
		t.Fatal(err)
	}
	img := picture(t)
	if _, err := m.run(context.Background(), key, "cover", func(context.Context) ([]byte, error) { return img, nil }); err != nil {
		t.Fatal(err)
	}
	closeResource(t, m)
}
