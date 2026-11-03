package resourcecache

import (
	"errors"

	"github.com/voici5986/otoneigo/internal/dirlock"
)

func lockRoot(root string) (func() error, error) {
	unlock, err := dirlock.Acquire(root)
	if errors.Is(err, dirlock.ErrInUse) {
		return nil, ErrInUse
	}
	if err != nil {
		return nil, errCache
	}
	return func() error {
		if err := unlock(); err != nil {
			return errCache
		}
		return nil
	}, nil
}
