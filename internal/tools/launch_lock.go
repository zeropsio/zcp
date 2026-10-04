package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// launchLock is a held advisory lock on one launch: every call that reads a
// launch's state and may mutate it holds it for the call's whole duration, so
// two calls — in one zcp process or two — never both pass the resume gate.
// The lock goes with the call that holds it, or with its process: a
// `launching` state found while the lock is free is one whose call ended.
// Release it exactly once.
type launchLock struct {
	f *os.File
}

// launchLockDir holds one lock file per launch. The state file is replaced
// by rename on every write and so cannot carry a lock itself; the locks live
// apart from the state files, so a state directory that cannot be written
// fails at the state write that names it.
const launchLockDir = "launch-production-locks"

// errLaunchHeld is another call holding the launch.
var errLaunchHeld = errors.New("another call holds the launch")

// tryLaunchLock takes the launch's lock without waiting: errLaunchHeld while
// another call holds it.
func tryLaunchLock(stateDir, launchID string) (*launchLock, error) {
	path := filepath.Join(stateDir, launchLockDir, launchID+".lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create launch lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open launch lock: %w", err)
	}
	ok, err := tryLockFile(f)
	if err != nil || !ok {
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("take launch lock: %w", err)
		}
		return nil, errLaunchHeld
	}
	return &launchLock{f: f}, nil
}

// release unlocks and closes the lock file.
func (l *launchLock) release() {
	_ = unlockFile(l.f)
	_ = l.f.Close()
}
