//go:build !windows

package effects

import (
	"errors"
	"os"
	"syscall"
)

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		// File fsync and atomic rename already completed. A filesystem that does
		// not implement directory fsync cannot use its absence as state evidence.
		if errors.Is(err, os.ErrInvalid) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return err
	}
	return nil
}
