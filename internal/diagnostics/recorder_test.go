package diagnostics

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecorderPersistsOnlyFixedSanitizedMetadata(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	secret := "https://cdn.example/media?token=SECRET_COOKIE_987"
	r.Record(Event{Stage: secret, SongID: "not-an-id-" + secret, Country: secret, Strategy: secret, Provider: secret, Status: secret, RequestedQuality: secret, ActualQuality: secret, ActualFormat: secret, ActualBitrate: -1, ErrorCode: secret, ElapsedMs: -8})
	r.Record(Event{Stage: "resolve", SongID: "123", Country: "US", Strategy: "normal", Provider: "netease-direct", Status: "full", RequestedQuality: "standard", ActualQuality: "higher", ActualFormat: "mp3", ActualBitrate: 192000, ErrorCode: "TIMEOUT", ElapsedMs: 14})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "playback.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET_COOKIE_987") || strings.Contains(string(b), "cdn.example") {
		t.Fatal("untrusted URL/token value persisted")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	lines := 0
	for scanner.Scan() {
		lines++
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid UTF-8 JSON line: %v", err)
		}
		if lines == 1 && (event.ActualFormat != "unknown" || event.ActualBitrate != 0 || event.ErrorCode != "UNKNOWN") {
			t.Fatalf("unsafe metadata persisted: %#v", event)
		}
		if lines == 2 && (event.ActualFormat != "mp3" || event.ActualBitrate != 192000) {
			t.Fatalf("actual quality metadata lost: %#v", event)
		}
		if event.Time <= 0 {
			t.Fatalf("missing recorder timestamp: %#v", event)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != 2 {
		t.Fatalf("got %d records", lines)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderConcurrentWritesAndNilCloseAreSafe(t *testing.T) {
	var nilRecorder *Recorder
	nilRecorder.Record(Event{Stage: "resolve"})
	if err := nilRecorder.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const workers, each = 8, 150
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				r.Record(Event{Stage: "resolve", SongID: "123", Country: "US", Status: "full"})
			}
		}()
	}
	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "playback.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(b), "\n")
	if lines != workers*each {
		t.Fatalf("lost or interleaved records: got %d want %d", lines, workers*each)
	}
	r.Record(Event{Stage: "resolve"})
}

func TestRecorderRotatesAtOneMiBAndKeepsOnlyCurrentAndPrevious(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 22000; i++ {
		r.Record(Event{Stage: "resolve", SongID: "123", Country: "US", Status: "full", ElapsedMs: int64(i)})
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"playback.jsonl", "playback.jsonl.old"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("rotation file %s: %v", name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 2 {
		t.Fatalf("rotation retained %d files, want at most 2", len(entries))
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 1024*1024 {
			t.Fatalf("%s exceeds 1 MiB: %d", e.Name(), info.Size())
		}
	}
}

func TestSanitizePreservesKnownPlaybackFailureCodes(t *testing.T) {
	for _, code := range []string{
		"EMPTY_URL", "UNAUTHORIZED", "RESOLVE_TIMEOUT", "GD_METADATA_UNVERIFIED",
		"GD_REQUEST_FAILED", "GD_DURATION_UNVERIFIED", "GD_MEDIA_UNVERIFIED",
		"GD_QUALITY_UNAVAILABLE", "GD_UNSUPPORTED_FORMAT", "QUALITY_NOT_ALLOWED",
		"GD_API_REDIRECT", "PROVIDER_REDIRECT_REJECTED", "COMPATIBILITY_ENDPOINT_REJECTED",
		"COMPATIBILITY_COOLDOWN",
		"GD_UNAVAILABLE", "GD_RATE_LIMITED", "PROVIDER_UNAVAILABLE", "TIMEOUT", "CANCELLED", "LOCAL_RECORDS_QUARANTINED",
	} {
		t.Run(code, func(t *testing.T) {
			if got := sanitize(Event{ErrorCode: code}).ErrorCode; got != code {
				t.Fatalf("got %q, want %q", got, code)
			}
		})
	}
	for _, code := range []string{"EMPTY_URL https://cdn.example/?token=secret", "GD_REQUEST_FAILED\nsecret", "NEW_PROVIDER_ERROR", "unauthorized"} {
		if got := sanitize(Event{ErrorCode: code}).ErrorCode; got != "UNKNOWN" {
			t.Fatalf("unrecognized code %q became %q", code, got)
		}
	}
}

func TestSanitizeBoundsActualQualityMetadata(t *testing.T) {
	for _, format := range []string{"mp3", "flac", "aac", "m4a", "wav", "ogg", "opus", "unknown", ""} {
		if got := sanitize(Event{ActualFormat: format}).ActualFormat; got != format {
			t.Fatalf("format %q became %q", format, got)
		}
	}
	for _, format := range []string{"https://cdn.example/?token=secret", "mp3\nsecret", "audio/mpeg", "MP3"} {
		if got := sanitize(Event{ActualFormat: format}).ActualFormat; got != "unknown" {
			t.Fatalf("unrecognized format %q became %q", format, got)
		}
	}
	for _, tc := range []struct{ input, want int64 }{
		{-1, 0}, {0, 0}, {128000, 128000}, {9216000, 9216000},
		{100000000, 100000000}, {100000001, 0}, {1<<63 - 1, 0},
	} {
		if got := sanitize(Event{ActualBitrate: tc.input}).ActualBitrate; got != tc.want {
			t.Fatalf("bitrate %d became %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestRecorderRotationFailureRecoversAndCloseReportsFailure(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// A nonempty directory deterministically prevents replacement on Windows and Unix.
	old := filepath.Join(dir, "playback.jsonl.old")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(old, "blocker")
	if err := os.WriteFile(blocker, []byte("private-path-token"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.file.Write([]byte(strings.Repeat(" ", 1024*1024-1))); err != nil {
		t.Fatal(err)
	}
	r.size = 1024*1024 - 1
	r.Record(Event{Stage: "resolve", SongID: "1"})
	for i := 0; i < 100; i++ {
		r.Record(Event{Stage: "resolve", SongID: "2"})
	}
	status := r.Status()
	if status.Code != "DIAGNOSTICS_IO_FAILED" || status.DroppedCount != 101 {
		t.Fatalf("missing bounded failure status: %+v", status)
	}
	if r.closed || r.file == nil {
		t.Fatal("rotation failure terminated recorder")
	}
	info, err := os.Stat(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1024*1024 {
		t.Fatalf("blocked rotation grew log: %d", info.Size())
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	r.Record(Event{Stage: "resolve", SongID: "3"})
	b, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"songId":"3"`) {
		t.Error("recorder stayed disabled after rotation obstacle was removed")
	}
	if got := r.Status(); got.Code != "" || got.DroppedCount != 101 {
		t.Errorf("recovery status: %+v", got)
	}
	if err := r.Close(); err == nil {
		t.Error("Close hid rotation failure")
	} else if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "private-path-token") {
		t.Error("Close exposed raw I/O data")
	}
}

func TestRecorderWriteFailureRetriesAndRetainsSanitizedCloseError(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// A stale/failed OS handle must be reopened by a later Record call.
	if err := r.file.Close(); err != nil {
		t.Fatal(err)
	}
	r.Record(Event{Stage: "resolve", SongID: "1"})
	if got := r.Status(); got.Code != "DIAGNOSTICS_IO_FAILED" || got.DroppedCount != 1 {
		t.Fatalf("write failure status: %+v", got)
	}
	r.Record(Event{Stage: "resolve", SongID: "2"})
	if got := r.Status(); got.DroppedCount != 2 {
		t.Fatalf("retry cooldown did not drop record: %+v", got)
	}
	// Advance the retry boundary without waiting for wall time.
	r.retryAfter = time.Now().Add(-time.Second)
	r.Record(Event{Stage: "storage", ErrorCode: "LOCAL_RECORDS_QUARANTINED"})
	b, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"stage":"storage"`) || !strings.Contains(string(b), `"errorCode":"LOCAL_RECORDS_QUARANTINED"`) {
		t.Fatalf("recovery/storage event missing: %s", b)
	}
	if got := r.Status(); got.Code != "" || got.DroppedCount != 2 {
		t.Fatalf("recovery status: %+v", got)
	}
	for i := 0; i < 2; i++ {
		if err := r.Close(); err == nil || err.Error() != "DIAGNOSTICS_IO_FAILED" {
			t.Fatalf("latched error lost: %v", err)
		}
	}
}

func TestRecorderReopenFailureRemainsRetryable(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.file.Close(); err != nil {
		t.Fatal(err)
	}
	r.file = nil
	if err := os.Remove(r.path); err != nil {
		t.Fatal(err)
	}
	// A directory at the active file path prevents reopening on every platform.
	if err := os.Mkdir(r.path, 0700); err != nil {
		t.Fatal(err)
	}
	r.Record(Event{Stage: "resolve", SongID: "1"})
	if r.closed {
		t.Fatal("reopen failure terminated recorder")
	}
	if got := r.Status(); got.Code != "DIAGNOSTICS_IO_FAILED" || got.DroppedCount != 1 {
		t.Fatalf("reopen status: %+v", got)
	}
	if err := os.Remove(r.path); err != nil {
		t.Fatal(err)
	}
	r.retryAfter = time.Now().Add(-time.Second)
	r.Record(Event{Stage: "resolve", SongID: "2"})
	b, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"songId":"2"`) {
		t.Fatal("reopening did not recover")
	}
	if got := r.Status(); got.Code != "" || got.DroppedCount != 1 {
		t.Fatalf("recovery status: %+v", got)
	}
	if err := r.Close(); err == nil || err.Error() != "DIAGNOSTICS_IO_FAILED" {
		t.Fatalf("reopen failure not observable: %v", err)
	}
}
