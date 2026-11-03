package mediacache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/crashreport"
	"github.com/voici5986/otoneigo/internal/domain"
)

type panicAudioBody struct{}

func (panicAudioBody) Read([]byte) (int, error) { panic("Cookie=private https://secret.invalid") }
func (panicAudioBody) Close() error             { return nil }

func TestFetchPanicSettlesAudioJobAndAllowsRetry(t *testing.T) {
	for _, duringRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "body-read"}[duringRead], func(t *testing.T) {
			logDir := t.TempDir()
			r, err := crashreport.Open(logDir)
			if err != nil {
				t.Fatal(err)
			}
			previous := crashreport.Install(r)
			defer crashreport.Install(previous)
			payload := fixture()
			var calls atomic.Int32
			m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					if !duringRead {
						panic("Token=private https://secret.invalid")
					}
					return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: panicAudioBody{}}, nil
				}
				return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Body: io.NopCloser(bytes.NewReader(payload))}, nil
			})
			stream, err := m.Prepare(context.Background(), candidate())
			if err != nil {
				t.Fatal(err)
			}
			response := get(t, stream.URL, "")
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != 502 {
				t.Fatalf("panic response status = %d", response.StatusCode)
			}
			m.Release(stream.CacheToken)
			waitDone(t, m)
			if m.Stats().CachedTracks != 0 {
				t.Fatal("failed job published a cache entry")
			}
			stream, err = m.Prepare(context.Background(), candidate())
			if err != nil {
				t.Fatal(err)
			}
			response = get(t, stream.URL, "")
			got, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatal("retry did not recover", err)
			}
			waitDone(t, m)
			if m.Stats().CachedTracks != 1 {
				t.Fatal("retry not committed")
			}
			m.Release(stream.CacheToken)
			done := make(chan error, 1)
			go func() { done <- m.Close() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("panic stranded Close")
			}
			reports, err := filepath.Glob(filepath.Join(logDir, "crash-*.txt"))
			if err != nil || len(reports) != 1 {
				t.Fatal("missing single report", err, len(reports))
			}
			data, err := os.ReadFile(reports[0])
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "audio-fetch") {
				t.Fatal("missing component")
			}
			for _, secret := range []string{"private", "secret.invalid", "Cookie=", "Token=", stream.CacheToken} {
				if bytes.Contains(data, []byte(secret)) {
					t.Fatal("private data in report")
				}
			}
		})
	}
}

func TestAudioInvariantPanicProcess(t *testing.T) {
	root := os.Getenv("OTONEIGO_AUDIO_PANIC_ROOT")
	if root == "" {
		return
	}
	r, err := crashreport.Open(filepath.Join(root, "log"))
	if err != nil {
		t.Fatal(err)
	}
	crashreport.Install(r)
	poison := false
	m, err := New(Options{Root: filepath.Join(root, "cache"), Entitlement: func() domain.PlaybackEntitlement {
		if poison {
			panic("Token=private")
		}
		return domain.PlaybackEntitlement{}
	}})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.Prepare(context.Background(), candidate())
	if err != nil {
		t.Fatal(err)
	}
	poison = true
	m.serve(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, stream.URL, nil))
	t.Fatal("fatal handler returned")
}

func TestAudioInvariantPanicRecordsBeforePinCleanup(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAudioInvariantPanicProcess$")
	cmd.Env = append(os.Environ(), "OTONEIGO_AUDIO_PANIC_ROOT="+root)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("fatal result: %v %s", err, output)
	}
	files, _ := filepath.Glob(filepath.Join(root, "log", "crash-*.txt"))
	if len(files) != 1 {
		t.Fatal("missing fatal report")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "component: audio-handler") || strings.Contains(string(data)+string(output), "Token=private") {
		t.Fatal("invalid fatal report")
	}
}
