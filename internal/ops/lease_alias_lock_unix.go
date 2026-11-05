//go:build unix

package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireLeaseAliasLock takes an exclusive advisory lock on the lease
// directory so one render-and-install of the alias file runs at a time,
// across this process's concurrent tool calls and `zcp init`. Without it two
// overlapping installs can land in the wrong order and the file loses a
// stanza. The caller MUST call the returned release function.
func acquireLeaseAliasLock(ctx context.Context, stateDir string) (func(), error) {
	dir := leaseDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ".aliases.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open alias lock: %w", err)
	}
	// A receipt lets an observer know the lock actually blocked, rather than
	// guessing from elapsed time. Ordinary calls use the blocking lock directly.
	var lockErr error
	if observer, ok := ctx.(interface{ aliasLockContended() }); ok {
		lockErr = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(lockErr, unix.EWOULDBLOCK) || errors.Is(lockErr, unix.EAGAIN) {
			observer.aliasLockContended()
			lockErr = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		}
	} else {
		lockErr = unix.Flock(int(f.Fd()), unix.LOCK_EX)
	}
	if lockErr != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquire alias lock: %w", lockErr)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
