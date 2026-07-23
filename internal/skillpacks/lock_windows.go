//go:build windows

package skillpacks

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile attempts a non-blocking exclusive LockFileEx on f. ok=false
// with a nil error means "already held elsewhere" (ERROR_LOCK_VIOLATION) —
// a normal contended-lock outcome the caller polls on, not a failure.
func tryLockFile(f *os.File) (ok bool, err error) {
	overlapped := new(windows.Overlapped)
	lockErr := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	if lockErr == nil {
		return true, nil
	}
	if errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, lockErr
}

func unlockFile(f *os.File) error {
	overlapped := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped)
}
