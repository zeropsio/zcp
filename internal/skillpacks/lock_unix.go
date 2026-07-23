//go:build unix

package skillpacks

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile attempts a non-blocking exclusive flock on f. ok=false with a
// nil error means "already held elsewhere" (EWOULDBLOCK/EAGAIN) — a normal
// contended-lock outcome the caller polls on, not a failure.
func tryLockFile(f *os.File) (ok bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
