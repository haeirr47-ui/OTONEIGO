//go:build windows

package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsLockedPreviousLogRecoversWithoutPermanentClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playback.jsonl")
	if err := os.WriteFile(path, make([]byte, maxRecordBytes), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".old", []byte("old fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path + ".old")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	r.Record(Event{Stage: "resolve", SongID: "123"})
	if s := r.Status(); s.Code == "" || s.DroppedCount != 1 {
		t.Fatal("locked previous log was not observed", s)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.retryAfter = time.Time{}
	r.mu.Unlock()
	r.Record(Event{Stage: "resolve", SongID: "456"})
	if s := r.Status(); s.Code != "" || s.DroppedCount != 1 {
		t.Fatal("log did not recover after unlocking", s)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e Event
	if json.Unmarshal(b, &e) != nil || e.SongID != "456" {
		t.Fatal("new record was lost")
	}
	if err = r.Close(); err == nil {
		t.Fatal("Close hid the earlier loss")
	}
}
