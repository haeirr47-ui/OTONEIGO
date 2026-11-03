package resourcecache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockRootErrorMapping(t *testing.T) {
	root := t.TempDir()
	unlock, err := lockRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			_ = unlock()
		}
	}()
	if second, err := lockRoot(root); second != nil || err != ErrInUse {
		if second != nil {
			_ = second()
		}
		t.Fatalf("busy lock must retain cache sentinel: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	released = true
	if err := unlock(); err != errCache {
		t.Fatalf("release failure must retain cache sentinel: %v", err)
	}
	invalid := t.TempDir()
	if err := os.Mkdir(filepath.Join(invalid, "cache.lock"), 0700); err != nil {
		t.Fatal(err)
	}
	if release, err := lockRoot(invalid); release != nil || err != errCache {
		if release != nil {
			_ = release()
		}
		t.Fatalf("invalid path must retain cache sentinel: %v", err)
	}
}
