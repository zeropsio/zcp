package conformance

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// LockKey is the reserved kv key the cross-process run lock uses. It MUST
// stay outside every fixture namespace's sweep pattern (seed.Cleanup SCANs
// and deletes `<namespace>:*` — a lock living there would be deleted by the
// run's OWN sweep/teardown mid-suite, silently disabling mutual exclusion;
// docs/spec-dataconsole-testing.md §5). `TestLock_KeyOutsideSweepPattern`
// pins this against DefaultNamespace; a custom DC_LIVE_NAMESPACE equal to
// "dclive-runlock"'s prefix is nonsensical and not defended against.
const LockKey = "dclive-runlock"

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

// releaseScript is an atomic compare-and-delete: the key is deleted ONLY
// when it still stores this run's runID. A bare DEL would delete a
// SUCCESSOR's lock whenever this run outlived its TTL (the TTL expired, a
// new run legitimately acquired) — re-opening the exact interleaving the
// lock exists to prevent.
var releaseScript = goredis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// Release compare-and-deletes the lock key: a no-op when the key is absent
// (TTL already expired) or held by ANOTHER run (this run's TTL expired and a
// successor won it). The TTL remains the stale-lock safety net a missed
// Release falls back to.
func (l *RunLock) Release(ctx context.Context) error {
	if err := releaseScript.Run(ctx, l.cli, []string{l.key}, l.runID).Err(); err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("conformance: run lock: compare-and-delete: %w", err)
	}
	return nil
}
