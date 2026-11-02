//go:build !windows

package resourcecache

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(f *os.File) (func() error, error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrInUse
		}
		return nil, errCache
	}
	return func() error {
		err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		ce := f.Close()
		if err != nil || ce != nil {
			return errCache
		}
		return nil
	}, nil
}
