//go:build windows

package mediacache

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsDeletionFailureRetainsAccountingAndRetries(t *testing.T) {
	p := fixture()
	m := newTest(t, t.TempDir(), func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p)), Body: io.NopCloser(bytes.NewReader(p))}, nil
	})
	s, _ := m.Prepare(context.Background(), candidate())
	r := get(t, s.URL, "")
	_, _ = io.Copy(io.Discard, r.Body)
	r.Body.Close()
	waitDone(t, m)
	held, e := os.Open(m.blob(s.CacheEntryID))
	if e != nil {
		t.Fatal(e)
	}
	defer held.Close()
	if _, e = m.Clear(); e != nil {
		t.Fatal(e)
	}
	m.Release(s.CacheToken)
	if _, e = os.Stat(m.blob(s.CacheEntryID)); e != nil || m.Stats().UsedBytes != int64(len(p)) {
		t.Fatal("failed unlink lost record/accounting", e, m.Stats())
	}
	held.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, e = os.Stat(m.blob(s.CacheEntryID)); os.IsNotExist(e) && m.Stats().UsedBytes == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pending unlink not retried", m.Stats())
}

func TestWindowsProgressiveReaderSurvivesCommitRename(t *testing.T) {
	part := filepath.Join(t.TempDir(), "audio.part")
	if err := os.WriteFile(part, []byte("prefix"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := openStreamReader(part)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w, err := os.OpenFile(part, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("suffix")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err = os.Rename(part, part+".blob"); err != nil {
		t.Fatal("reader blocked commit rename", err)
	}
	got := make([]byte, 12)
	if _, err = r.ReadAt(got, 0); err != nil || string(got) != "prefixsuffix" {
		t.Fatal(string(got), err)
	}
}
