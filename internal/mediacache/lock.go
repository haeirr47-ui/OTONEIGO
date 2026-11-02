package mediacache

import (
	"os"
	"path/filepath"
)

// The OS releases this lifetime lock on process death. Never remove the lock
// file: replacing its inode while a process owns it would create two owners.
func lockRoot(root string) (func() error, error) {
	p := filepath.Join(root, "cache.lock")
	if st, e := os.Lstat(p); e == nil && !st.Mode().IsRegular() {
		return nil, errCache
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, errCache
	}
	unlock, e := lockFile(f)
	if e != nil {
		_ = f.Close()
		return nil, e
	}
	return unlock, nil
}
