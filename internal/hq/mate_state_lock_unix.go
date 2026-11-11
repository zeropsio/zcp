//go:build unix

package hq

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockMateState(f *os.File) error {
	unix.CloseOnExec(int(f.Fd()))
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func probeMateStateLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return false, err
	}
	return false, nil
}
