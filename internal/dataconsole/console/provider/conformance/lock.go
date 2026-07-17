package conformance

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// LockKey is the reserved kv key the cross-process run lock uses — outside
// the fixture namespace (DefaultNamespace / DC_LIVE_NAMESPACE) so it can
// never collide with a seeded fixture key (docs/spec-dataconsole-testing.md
// §5: "a dev run and a release run cannot interleave fixtures").
const LockKey = "dcconf:lock"

// Default bounds for RunLock.Acquire's poll and the SET NX EX TTL — the
// production values; lock_test.go constructs a RunLock with much smaller
// values directly (same package, unexported fields) so its "second acquire
// blocks then times out" case doesn't need a real 60s wait.
const (
	defaultLockTimeout = 60 * time.Second
	defaultLockPollGap = 2 * time.Second
	defaultLockTTL     = 10 * time.Minute
)

// ErrLockTimeout is wrapped into Acquire's error once the bounded poll
// expires still contended.
var ErrLockTimeout = errors.New("conformance: run lock: acquisition timed out")

// RunLock is the cross-process project-scoped lock (§5): SET NX EX on a
// reserved key so a dev run and a release run against the same testbed can
// never interleave fixture seed/cleanup. The TTL is the crash-recovery bound
// — a run that dies mid-suite (kill -9, VPN drop) releases the lock
// automatically after ttl elapses, rather than requiring a manual DEL.
type RunLock struct {
	cli     *goredis.Client
	key     string
	runID   string
	ttl     time.Duration
	timeout time.Duration
	pollGap time.Duration
}

// NewRunLock constructs a RunLock bound to cli/runID, keyed by LockKey, using
// the production timeout/poll/TTL bounds.
func NewRunLock(cli *goredis.Client, runID string) *RunLock {
	return &RunLock{cli: cli, key: LockKey, runID: runID, ttl: defaultLockTTL, timeout: defaultLockTimeout, pollGap: defaultLockPollGap}
}

// Acquire polls SET NX EX until it wins the key or l.timeout elapses, at
// l.pollGap. A won lock stores l.runID as the value, so a losing caller's
// timeout error can name the holder. ctx cancellation aborts early.
func (l *RunLock) Acquire(ctx context.Context) error {
	deadline := time.Now().Add(l.timeout)
	for {
		ok, err := l.cli.SetNX(ctx, l.key, l.runID, l.ttl).Result()
		if err != nil {
			return fmt.Errorf("conformance: run lock: SET NX EX: %w", err)
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			holder, _ := l.cli.Get(ctx, l.key).Result() // best-effort — a concurrent expiry here is not a new finding
			if holder == "" {
				holder = "unknown"
			}
			return fmt.Errorf("%w: held by run %q", ErrLockTimeout, holder)
		}
		select {
		case <-time.After(l.pollGap):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Release deletes the lock key unconditionally — best-effort: a delete after
// TTL expiry, or one this process never actually won, is a silent no-op (the
// TTL is the stale-lock safety net a missed Release falls back to).
func (l *RunLock) Release(ctx context.Context) error {
	if err := l.cli.Del(ctx, l.key).Err(); err != nil {
		return fmt.Errorf("conformance: run lock: DEL: %w", err)
	}
	return nil
}
