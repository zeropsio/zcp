//go:build unix

package mate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// The install lock is what `zcp service start mate` and `zcp init` share, so
// one install per update and one server start: whichever holds it installs,
// the other waits and then finds nothing to do. It never blocks a start
// forever: a wait is bounded, and a lock nobody holds — a file left on disk,
// a holder that died — is free.
func TestLockInstall(t *testing.T) {
	// Not parallel: HOME is process-wide.
	t.Setenv("HOME", t.TempDir())

	release, err := mate.LockInstall(time.Second)
	if err != nil {
		t.Fatalf("an unheld lock: %v", err)
	}
	start := time.Now()
	if _, err := mate.LockInstall(300 * time.Millisecond); !errors.Is(err, mate.ErrInstallLockBusy) {
		t.Fatalf("a held lock: err = %v, want ErrInstallLockBusy", err)
	}
	if waited := time.Since(start); waited < 250*time.Millisecond || waited > 3*time.Second {
		t.Errorf("waited %s for a held lock, want the bounded wait", waited)
	}
	release()
	release() // releasing twice is harmless

	again, err := mate.LockInstall(0)
	if err != nil {
		t.Fatalf("a released lock: %v", err)
	}
	again()

	// The lock file stays on disk, and a file nobody holds a lock on is free.
	if _, err := os.Stat(filepath.Join(mate.Prefix(), "install.lock")); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	stale, err := mate.LockInstall(0)
	if err != nil {
		t.Fatalf("a stale lock file must not block: %v", err)
	}
	stale()
}

// A holder that waits on the lock gets it the moment the holder lets go.
func TestLockInstall_AWaiterGetsItOnRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := mate.LockInstall(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		release()
	}()
	start := time.Now()
	got, err := mate.LockInstall(5 * time.Second)
	if err != nil {
		t.Fatalf("waiting on a lock released after 200ms: %v", err)
	}
	got()
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %s, want it taken soon after the release", waited)
	}
}
