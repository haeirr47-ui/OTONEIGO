//go:build !windows

package dirlock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File) (func() error, error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrInUse
		}
		return nil, errLock
	}
	return func() error {
		err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		ce := f.Close()
		if err != nil || ce != nil {
			return errLock
		}
		return nil
	}, nil
}
