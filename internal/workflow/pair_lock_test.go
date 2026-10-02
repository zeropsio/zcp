// Tests for: workflow/pair_lock.go — one process at a time runs git on a
// pair's checkout.
package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestLockPair: while one holder has a pair, another gets it only once the
// first lets go within its wait — not at all with no wait, ErrPairBusy past
// it, its context's error when that ends first. Another pair is never held up.
func TestLockPair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		hostname  string
		wait      time.Duration
		releaseIn time.Duration // 0: the first holder keeps it
		cancelIn  time.Duration // 0: the context does not end
		wantErr   error
	}{
		{name: "held, no wait", hostname: "appdev", wantErr: ErrPairBusy},
		{name: "held past the wait", hostname: "appdev", wait: 50 * time.Millisecond, wantErr: ErrPairBusy},
		{name: "let go within the wait", hostname: "appdev", wait: 5 * time.Second, releaseIn: 50 * time.Millisecond},
		{name: "the context ends first", hostname: "appdev", wait: 5 * time.Second, cancelIn: 50 * time.Millisecond, wantErr: context.Canceled},
		{name: "another pair", hostname: "apidev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			release, err := LockPair(t.Context(), stateDir, "appdev", 0)
			if err != nil {
				t.Fatalf("the first holder: %v", err)
			}
			if tt.releaseIn > 0 {
				time.AfterFunc(tt.releaseIn, release)
			} else {
				defer release()
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelIn > 0 {
				time.AfterFunc(tt.cancelIn, cancel)
			}

			started := time.Now()
			second, err := LockPair(ctx, stateDir, tt.hostname, tt.wait)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("the second holder: err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if errors.Is(err, ErrPairBusy) && time.Since(started) < tt.wait {
					t.Errorf("gave up after %v, before its wait of %v", time.Since(started), tt.wait)
				}
				return
			}
			second()
		})
	}
}

// TestLockPair_ReleasedIsFree: a pair let go of is free for the next holder
// at once.
func TestLockPair_ReleasedIsFree(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	for range 2 {
		release, err := LockPair(t.Context(), stateDir, "appdev", 0)
		if err != nil {
			t.Fatalf("LockPair: %v", err)
		}
		release()
	}
}
