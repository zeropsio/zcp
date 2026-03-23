//go:build windows

package workflow

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock    = 0x00000002
	lockfileFailImmediately  = 0x00000001
	errLockViolation         = 33
)

// lockFileExclusive acquires an exclusive lock via LockFileEx with timeout.
func lockFileExclusive(f *os.File) error {
	return lockWithRetry(f, lockfileExclusiveLock)
}

// lockFileShared acquires a shared (read-only) lock via LockFileEx with timeout.
func lockFileShared(f *os.File) error {
	return lockWithRetry(f, 0)
}

// lockWithRetry attempts a non-blocking LockFileEx with retries.
// Returns error after flockRetries * flockInterval (~5s).
func lockWithRetry(f *os.File, flags uintptr) error {
	for i := 0; i < flockRetries; i++ {
		var ol syscall.Overlapped
		r1, _, err := procLockFileEx.Call(
			f.Fd(),
			flags|lockfileFailImmediately,
			0,
			1, 0,
			uintptr(unsafe.Pointer(&ol)),
		)
		if r1 != 0 {
			return nil
		}
		if errno, ok := err.(syscall.Errno); !ok || errno != errLockViolation {
			return err
		}
		time.Sleep(flockInterval)
	}
	return fmt.Errorf("LockFileEx: timeout after %v waiting for registry lock", time.Duration(flockRetries)*flockInterval)
}

// unlockFile releases the lock via UnlockFileEx.
func unlockFile(f *os.File) {
	var ol syscall.Overlapped
	_, _, _ = procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
}

// isProcessAlive checks if a process with the given PID exists.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}
