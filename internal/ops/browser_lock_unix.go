//go:build unix

package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// browserLockPoll is how often a waiting call retries the browser lock.
	browserLockPoll = 50 * time.Millisecond
	// browserWalkShare: a waiting call gives up while 1/browserWalkShare of
	// its time is left (20 s of the default 120 s). One that got the browser
	// with less would be cut mid-walk and recycle the browser every call
	// shares, so it answers busy without touching it.
	browserWalkShare = 6
)

// waitBrowserLock takes the exclusive lock at path, waiting until ctx ends
// or only its walk's share of the time is left. flock is per open file, so
// it serialises calls in one process and across processes alike, and the
// kernel drops it when its holder dies.
func waitBrowserLock(ctx context.Context, path string) (func(), error) {
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-time.Until(deadline)/browserWalkShare))
		defer cancel()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("browser lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("browser lock: %w", err)
	}
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("browser lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("the browser is busy with another call for longer than this call can wait: %w", ctx.Err())
		case <-time.After(browserLockPoll):
		}
	}
}
