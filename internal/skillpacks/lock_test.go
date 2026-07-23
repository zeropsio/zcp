package skillpacks

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquirePackLock_SecondCallerTimesOutBusy(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	first, err := acquirePackLock(cwd, time.Second)
	if err != nil {
		t.Fatalf("first acquirePackLock: %v", err)
	}
	defer func() { _ = first.release() }()

	_, err = acquirePackLock(cwd, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected the second (short-timeout) acquirePackLock to fail while the first holds the lock")
	}
	var ce *CodedError
	if !errors.As(err, &ce) || ce.Code != CodeBusy {
		t.Errorf("error = %v, want a *CodedError with code %q", err, CodeBusy)
	}
}

func TestAcquirePackLock_ReleasedThenReacquirable(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	first, err := acquirePackLock(cwd, time.Second)
	if err != nil {
		t.Fatalf("first acquirePackLock: %v", err)
	}
	if err := first.release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	second, err := acquirePackLock(cwd, time.Second)
	if err != nil {
		t.Fatalf("second acquirePackLock after release: %v", err)
	}
	_ = second.release()
}

// TestAcquirePackLock_ConcurrentGoroutines_OnlyOneAtATime is the "same-pack
// and cross-pack concurrent processes" proof at the lock-primitive level:
// many goroutines racing for the same workspace lock never observe it held
// by two of them simultaneously.
func TestAcquirePackLock_ConcurrentGoroutines_OnlyOneAtATime(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()

	const n = 8
	results := make(chan bool, n)
	var holding int32
	for range n {
		go func() {
			l, err := acquirePackLock(cwd, 2*time.Second)
			if err != nil {
				results <- false
				return
			}
			ok := atomic.AddInt32(&holding, 1) == 1
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&holding, -1)
			_ = l.release()
			results <- ok
		}()
	}
	for range n {
		if !<-results {
			t.Error("more than one goroutine held the lock simultaneously")
		}
	}
}
