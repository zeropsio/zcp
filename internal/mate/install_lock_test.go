//go:build unix

package mate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
		release, err := mate.LockInstall(time.Second)
		if err != nil {
			t.Fatalf("an unheld lock: %v", err)
		}
		start := time.Now()
		if _, err := mate.LockInstall(300 * time.Millisecond); !errors.Is(err, mate.ErrInstallLockBusy) {
			t.Fatalf("a held lock: err = %v, want ErrInstallLockBusy", err)
		}
		if waited := time.Since(start); waited != 300*time.Millisecond {
			t.Errorf("virtual wait for a held lock = %s, want 300ms", waited)
		}
		release()
		release() // releasing twice is harmless

		again, err := mate.LockInstall(0)
		if err != nil {
			t.Fatalf("a released lock: %v", err)
		}
		again()
		if _, err := os.Stat(filepath.Join(mate.Prefix(), "install.lock")); err != nil {
			t.Fatalf("lock file: %v", err)
		}
		stale, err := mate.LockInstall(0)
		if err != nil {
			t.Fatalf("a stale lock file must not block: %v", err)
		}
		stale()
	})
}

// A holder that waits on the lock gets it the moment the holder lets go.
func TestLockInstall_AWaiterGetsItOnRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		release, err := mate.LockInstall(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			release func()
			err     error
		}
		resultCh := make(chan result, 1)
		go func() {
			got, err := mate.LockInstall(5 * time.Second)
			resultCh <- result{got, err}
		}()
		synctest.Wait() // the waiter has tried the held lock and is sleeping
		select {
		case got := <-resultCh:
			release()
			if got.release != nil {
				got.release()
			}
			t.Fatalf("waiter returned while the lock was held: %v", got.err)
		default:
		}
		release()
		time.Sleep(2 * time.Second) // the former wait bound, on synctest's clock
		synctest.Wait()
		select {
		case got := <-resultCh:
			if got.err != nil {
				t.Fatalf("waiting on a released lock: %v", got.err)
			}
			got.release()
		default:
			t.Error("the waiter did not take the released lock")
			time.Sleep(3 * time.Second) // let its own wait expire before leaving the bubble
			got := <-resultCh
			if got.release != nil {
				got.release()
			}
		}
	})
}
