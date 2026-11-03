// Package dirlock provides a process lifetime exclusive lock for a cache directory.
package dirlock

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrInUse means another owner already holds the directory lock.
var ErrInUse = errors.New("directory lock in use")

var errLock = errors.New("directory lock failed")

// Acquire locks root/cache.lock without waiting for another owner.
// The OS releases this lifetime lock on process death. Never remove the lock
// file: replacing its inode while a process owns it would create two owners.
func Acquire(root string) (func() error, error) {
	p := filepath.Join(root, "cache.lock")
	if st, e := os.Lstat(p); e == nil && !st.Mode().IsRegular() {
		return nil, errLock
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, errLock
	}
	unlock, e := lockFile(f)
	if e != nil {
		_ = f.Close()
		return nil, e
	}
	return unlock, nil
}
