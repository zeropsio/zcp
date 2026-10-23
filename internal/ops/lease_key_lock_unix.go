//go:build unix

package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// leaseKeyLockPoll is how often an acquire waiting for a key's lock tries it
// again.
const leaseKeyLockPoll = 50 * time.Millisecond

// lockLeaseKey takes the exclusive advisory lock at path, waiting until it is
// free or ctx ends. flock is per open file, so it serialises acquires of one
// key in this process and across zcp processes alike (every agent's zcp in the
// container shares the state directory), and the kernel drops it when its
// holder dies. The caller MUST call the returned release function.
func lockLeaseKey(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lease lock: %w", err)
	}
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("take lease lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("wait for lease lock: %w", ctx.Err())
		case <-time.After(leaseKeyLockPoll):
		}
	}
}
