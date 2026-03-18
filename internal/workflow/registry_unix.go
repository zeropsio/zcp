//go:build !windows

package workflow

import (
	"os"
	"syscall"
)

// lockFileExclusive acquires an exclusive flock on the file.
func lockFileExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// lockFileShared acquires a shared (read-only) flock on the file.
func lockFileShared(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH)
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
