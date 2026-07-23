package skillpacks

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lockRelPath is the one cross-process advisory lock every pack-add/
// pack-remove holds for its full duration. A single lock covers ALL packs
// (not one per pack id) because two different packs can claim the same
// destination name, so a per-pack lock would not prevent a collision
// between two concurrent installs.
const lockRelPath = ".zcp/state/skill-packs.lock"

// lockAcquireTimeout bounds how long a caller waits for the lock before
// giving up with CodeBusy.
const lockAcquireTimeout = 5 * time.Second

const lockPollInterval = 50 * time.Millisecond

// packLock is a held advisory lock; release it exactly once via release().
type packLock struct {
	f *os.File
}

// acquirePackLock opens (creating if needed) cwd's lock file and polls,
// at lockPollInterval, until it obtains an exclusive lock or timeout
// elapses. tryLockFile/unlockFile are implemented per-OS (lock_unix.go via
// flock, lock_windows.go via LockFileEx).
func acquirePackLock(cwd string, timeout time.Duration) (*packLock, error) {
	path := filepath.Join(cwd, lockRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, wrapCoded(CodeFilesystem, err, "create skill-packs lock directory")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, wrapCoded(CodeFilesystem, err, "open skill-packs lock file")
	}

	deadline := time.Now().Add(timeout)
	for {
		ok, lockErr := tryLockFile(f)
		if lockErr != nil {
			_ = f.Close()
			return nil, wrapCoded(CodeFilesystem, lockErr, "acquire skill-packs lock")
		}
		if ok {
			return &packLock{f: f}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, codedErrorf(CodeBusy, "another skill-pack operation is running in this workspace")
		}
		time.Sleep(lockPollInterval)
	}
}

// release unlocks and closes the lock file. It is safe to call at most once.
func (l *packLock) release() error {
	unlockErr := unlockFile(l.f)
	closeErr := l.f.Close()
	if unlockErr != nil {
		return fmt.Errorf("release skill-packs lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close skill-packs lock file: %w", closeErr)
	}
	return nil
}
