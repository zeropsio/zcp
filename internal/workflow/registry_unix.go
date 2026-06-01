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

// isProcessAlive reports whether the SAME process that recorded recordedStart is
// still running as pid. Two-state: a PID that exists but whose start-time no
// longer matches recordedStart is a RECYCLED PID → dead (defeats the
// operator-wedge / stale-session class). An empty recordedStart (legacy session,
// or a platform without start-time support) trusts the bare PID. If the PID
// exists but its start-time is unreadable, bias ALIVE — never prune a live
// session over an unreadable clock.
func isProcessAlive(pid int, recordedStart string) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
		return false // no such process
	}
	if recordedStart == "" {
		return true
	}
	if cur, ok := processStartTime(pid); ok {
		return cur == recordedStart
	}
	return true
}
