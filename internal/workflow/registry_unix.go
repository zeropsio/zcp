//go:build !windows

package workflow

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// lockFileExclusive acquires an exclusive flock on the file with timeout.
func lockFileExclusive(f *os.File) error {
	return lockWithRetry(f, syscall.LOCK_EX)
}

// lockFileShared acquires a shared (read-only) flock on the file with timeout.
func lockFileShared(f *os.File) error {
	return lockWithRetry(f, syscall.LOCK_SH)
}

// lockWithRetry attempts a non-blocking flock with retries.
// Returns error after flockRetries * flockInterval (~5s).
func lockWithRetry(f *os.File, how int) error {
	for range flockRetries {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("flock: %w", err)
		}
		time.Sleep(flockInterval)
	}
	return fmt.Errorf("flock: timeout after %v waiting for registry lock", time.Duration(flockRetries)*flockInterval)
}

// unlockFile releases the flock.
func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// isProcessAlive checks if a process with the given PID exists.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
