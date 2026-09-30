package mate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The install lock is held across a bundle install and whatever must not race
// it. Two processes install a Mate's bundle: `zcp service start mate`, the
// unit's own start, which runs at boot on its own, and `zcp init`, which runs
// alongside it. Each takes this lock around EnsureInstalled, so whichever runs
// first installs the release and the other finds nothing to do: the server
// starts once, on the new release, and `zcp init` has nothing to restart.
//
// It is an flock on a file under Prefix(): the kernel releases it when its
// holder exits, however it exits, so a lock nobody holds — a file left on
// disk, a holder that died — never blocks anyone. A live holder is waited for
// at most the caller's bound; past it the caller goes on without the lock.

// ErrInstallLockBusy is a lock still held when the wait ran out.
var ErrInstallLockBusy = errors.New("another mate install holds the install lock")

// installLockPoll is how often a waiter tries the lock again.
const installLockPoll = 100 * time.Millisecond

// installLockPath is the file the install lock is held on.
func installLockPath() string { return filepath.Join(Prefix(), "install.lock") }

// LockInstall takes the install lock, waiting at most wait for a holder to let
// go, and returns the function that releases it (safe to call more than
// once). ErrInstallLockBusy when the wait ran out.
func LockInstall(wait time.Duration) (func(), error) {
	if err := os.MkdirAll(Prefix(), 0o755); err != nil {
		return nil, fmt.Errorf("install lock: mkdir %s: %w", Prefix(), err)
	}
	f, err := os.OpenFile(installLockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("install lock: open %s: %w", installLockPath(), err)
	}
	deadline := time.Now().Add(wait)
	for {
		ok, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("install lock: %s: %w", installLockPath(), err)
		}
		if ok {
			var once sync.Once
			return func() {
				once.Do(func() {
					_ = unlockFile(f)
					_ = f.Close()
				})
			}, nil
		}
		left := time.Until(deadline)
		if left <= 0 {
			_ = f.Close()
			return nil, ErrInstallLockBusy
		}
		time.Sleep(min(installLockPoll, left))
	}
}
