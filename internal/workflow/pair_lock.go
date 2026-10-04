package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrPairBusy is LockPair's answer when another holder keeps the pair past
// the wait.
var ErrPairBusy = errors.New("another process is running git on the pair's checkout")

// LockPair holds the pair's checkout for one process at a time: two git
// sequences on one checkout — a delivery's commit and another's merge of
// `main` — can leave it half-merged, and git's own index.lock only catches
// the moment they overlap. It is an exclusive file lock beside the pair's
// meta, so it holds across the zcp of every agent, and between two holders
// in one process too: a holder must never take it again while it has it.
//
// A holder waits up to wait for another to let go (0: not at all), then
// gives up with ErrPairBusy; ctx ending gives up sooner. release lets go.
func LockPair(ctx context.Context, stateDir, hostname string, wait time.Duration) (release func(), err error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("pair lock mkdir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(stateDir, ".pair-"+hostname+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("pair lock open: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		held, err := tryLockExclusive(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if held {
			return func() {
				unlockFile(f)
				_ = f.Close()
			}, nil
		}
		left := time.Until(deadline)
		if left <= 0 {
			_ = f.Close()
			return nil, ErrPairBusy
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(min(flockInterval, left)):
		}
	}
}
