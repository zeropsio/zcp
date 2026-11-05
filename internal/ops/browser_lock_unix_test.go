//go:build unix

package ops

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// lockingRunner takes the real browser lock in Acquire, as the production
// runner does, and calls onRun inside Run so tests can observe overlap.
type lockingRunner struct {
	lock      string
	onAcquire func()
	onRun     func()
}

func (*lockingRunner) LookPath() (string, error) { return "/fake/agent-browser", nil }

func (l *lockingRunner) Acquire(ctx context.Context, _ string) (func(bool) error, bool, error) {
	if l.onAcquire != nil {
		l.onAcquire()
	}
	unlock, err := waitBrowserLock(ctx, l.lock)
	if err != nil {
		return nil, false, err
	}
	return func(bool) error { unlock(); return nil }, false, nil
}

func (l *lockingRunner) Run(_ context.Context, _ string, _ time.Duration) (string, string, bool, error) {
	if l.onRun != nil {
		l.onRun()
	}
	return `[]`, "", false, nil
}

// TestBrowserBatch_SerializesCalls spawns several concurrent callers and
// asserts they execute sequentially inside Run. This is the load-bearing
// safety property: one walk at a time on the container's browser, because
// agent-browser drives one active tab per daemon.
func TestBrowserBatch_SerializesCalls(t *testing.T) {
	const concurrency = 5
	lock := filepath.Join(t.TempDir(), "lock")
	synctest.Test(t, func(t *testing.T) {
		var inFlight, maxInFlight atomic.Int32
		attempted := make(chan struct{}, concurrency)
		entered := make(chan struct{}, concurrency)
		release := make(chan struct{}, concurrency)
		runner := &lockingRunner{
			lock:      lock,
			onAcquire: func() { attempted <- struct{}{} },
			onRun: func() {
				cur := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					oldMax := maxInFlight.Load()
					if cur <= oldMax || maxInFlight.CompareAndSwap(oldMax, cur) {
						break
					}
				}
				entered <- struct{}{}
				<-release
			},
		}
		defer OverrideBrowserRunnerForTest(runner)()

		var wg sync.WaitGroup
		start := func() {
			wg.Go(func() {
				_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
				if err != nil {
					t.Errorf("concurrent call errored: %v", err)
				}
			})
		}
		start()
		<-attempted
		<-entered
		for range concurrency - 1 {
			start()
		}
		for range concurrency - 1 {
			<-attempted
		}
		synctest.Wait()
		if got := maxInFlight.Load(); got != 1 {
			t.Errorf("max concurrent Run invocations while held = %d, want 1", got)
		}
		for range concurrency {
			release <- struct{}{}
		}
		wg.Wait()
		if got := maxInFlight.Load(); got != 1 {
			t.Errorf("max concurrent Run invocations = %d, want 1 — browser calls must be serialized", got)
		}
	})
}

// TestBrowserBatch_SecondCallerWaitsForTheBrowser pins that a call arriving
// while another one holds the browser waits for it instead of failing, and
// runs as soon as the first call ends.
func TestBrowserBatch_SecondCallerWaitsForTheBrowser(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "lock")
	synctest.Test(t, func(t *testing.T) {
		held, release := make(chan struct{}), make(chan struct{})
		attempted := make(chan struct{}, 2)
		var runs atomic.Int32
		runner := &lockingRunner{
			lock:      lock,
			onAcquire: func() { attempted <- struct{}{} },
			onRun: func() {
				if runs.Add(1) == 1 {
					close(held)
					<-release
				}
			},
		}
		defer OverrideBrowserRunnerForTest(runner)()

		first := make(chan error, 1)
		go func() {
			_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
			first <- err
		}()
		<-attempted
		<-held

		second := make(chan error, 1)
		go func() {
			_, err := BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://second.example.com"})
			second <- err
		}()
		<-attempted
		synctest.Wait()
		var secondErr error
		returned := false
		select {
		case secondErr = <-second:
			returned = true
			t.Errorf("second caller returned while the browser was held: %v", secondErr)
		default:
		}
		close(release)
		if !returned {
			secondErr = <-second
		}
		if secondErr != nil {
			t.Errorf("second caller failed after the browser was free: %v", secondErr)
		}
		if err := <-first; err != nil {
			t.Errorf("first caller: %v", err)
		}
		if got := runs.Load(); got != 2 {
			t.Errorf("runs = %d, want 2", got)
		}
	})
}

// TestBrowserBatch_CtxCancelWhileLockHeld verifies that a caller whose ctx
// is already cancelled returns immediately without waiting for the browser,
// even when another caller is mid-flight.
func TestBrowserBatch_CtxCancelWhileLockHeld(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "lock")
	synctest.Test(t, func(t *testing.T) {
		held, release := make(chan struct{}), make(chan struct{})
		runner := &lockingRunner{
			lock:  lock,
			onRun: func() { close(held); <-release },
		}
		defer OverrideBrowserRunnerForTest(runner)()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = BrowserBatch(context.Background(), BrowserBatchInput{URL: "https://example.com"})
		}()
		<-held
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		returned := make(chan error, 1)
		go func() {
			_, err := BrowserBatch(ctx, BrowserBatchInput{URL: "https://example.com"})
			returned <- err
		}()
		synctest.Wait()
		select {
		case err := <-returned:
			if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "busy") {
				t.Errorf("want a busy error wrapping the cancellation, got: %v", err)
			}
		default:
			t.Error("cancelled caller kept waiting for the browser")
		}
		close(release)
		<-done
	})
}

// TestBrowserBatch_WaitEndsAtTheCallDeadline pins that queue time counts
// against timeoutSeconds: a caller that cannot get the browser before its
// own deadline gets "the browser is busy", not an endless wait.
func TestBrowserBatch_WaitEndsAtTheCallDeadline(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "lock")
	unlock, err := waitBrowserLock(context.Background(), lock)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	defer OverrideBrowserRunnerForTest(&lockingRunner{lock: lock})()

	synctest.Test(t, func(t *testing.T) {
		const budget = 200 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		start := time.Now()
		_, err := BrowserBatch(ctx, BrowserBatchInput{URL: "https://example.com"})
		if err == nil || !strings.Contains(err.Error(), "busy") || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want a busy error at the deadline, got %v", err)
		}
		if waited, want := time.Since(start), budget-budget/6; waited != want {
			t.Errorf("waited %v, want %v of virtual time", waited, want)
		}
	})
}

// TestBrowserBatch_LateWaiterAnswersBusyWithoutTouchingTheBrowser pins that a
// waiting call gives up while a sixth of its time is left: one that got the
// browser with too little time would be cut mid-walk and recycle the warm
// browser everyone shares, so it answers busy and never runs.
func TestBrowserBatch_LateWaiterAnswersBusyWithoutTouchingTheBrowser(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "lock")
	unlock, err := waitBrowserLock(context.Background(), lock)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var runs atomic.Int32
	defer OverrideBrowserRunnerForTest(&lockingRunner{lock: lock, onRun: func() { runs.Add(1) }})()

	synctest.Test(t, func(t *testing.T) {
		const budget = time.Second
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		defer cancel()
		start := time.Now()
		_, err := BrowserBatch(ctx, BrowserBatchInput{URL: "https://example.com"})
		if err == nil || !strings.Contains(err.Error(), "busy") {
			t.Fatalf("want a busy error, got %v", err)
		}
		if runs.Load() != 0 {
			t.Error("a late waiter must not run on the browser")
		}
		if waited, want := time.Since(start), budget-budget/6; waited != want {
			t.Errorf("waited %v, want %v of virtual time", waited, want)
		}
	})
}
