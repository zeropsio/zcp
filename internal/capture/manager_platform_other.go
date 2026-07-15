//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package capture

import (
	"errors"
	"os"
)

func lockManagerFile(_ *os.File) error {
	return errors.New("persistent capture manager locking is unsupported on this platform")
}

func unlockManagerFile(_ *os.File) error { return nil }

func managerConnectionRefused(_ error) bool { return false }

func processAlive(_ int) bool { return false }
