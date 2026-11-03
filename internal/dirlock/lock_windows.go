//go:build windows

package dirlock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) (func() error, error) {
	var overlapped windows.Overlapped
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrInUse
		}
		return nil, errLock
	}
	return func() error {
		err := windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
		ce := f.Close()
		if err != nil || ce != nil {
			return errLock
		}
		return nil
	}, nil
}
