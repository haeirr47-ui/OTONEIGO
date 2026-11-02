//go:build windows

package mediacache

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func lockFile(f *os.File) (func() error, error) {
	var overlapped windows.Overlapped
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrInUse
		}
		return nil, errCache
	}
	return func() error {
		err := windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
		ce := f.Close()
		if err != nil || ce != nil {
			return errCache
		}
		return nil
	}, nil
}
