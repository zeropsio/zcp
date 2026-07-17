package conformance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// newLockTestClient spins a hermetic in-memory Valkey (miniredis, already a
// repo dependency — see provider/kv's redis tests) and returns a REAL
// *goredis.Client pointed at it, so RunLock is exercised against the exact
// same client type production uses, no mock/interface needed.
func newLockTestClient(t *testing.T) (*goredis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	cli := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	return cli, mr
}

func TestLock_AcquireConflictAndStale(t *testing.T) {
	t.Parallel()

	t.Run("second acquire blocks then errors naming the holder's runID", func(t *testing.T) {
		t.Parallel()
		cli, _ := newLockTestClient(t)

		first := &RunLock{cli: cli, key: LockKey, runID: "run-holder", ttl: time.Minute, timeout: time.Second, pollGap: 10 * time.Millisecond}
		if err := first.Acquire(context.Background()); err != nil {
			t.Fatalf("first Acquire: %v", err)
		}

		second := &RunLock{cli: cli, key: LockKey, runID: "run-contender", ttl: time.Minute, timeout: 150 * time.Millisecond, pollGap: 20 * time.Millisecond}
		start := time.Now()
		err := second.Acquire(context.Background())
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("second Acquire: want an error while the first holder's lock is still live, got nil")
		}
		if !errors.Is(err, ErrLockTimeout) {
			t.Errorf("second Acquire error = %v, want it to wrap ErrLockTimeout", err)
		}
		if !strings.Contains(err.Error(), "run-holder") {
			t.Errorf("second Acquire error %q must name the holder's runID (run-holder)", err.Error())
		}
		if elapsed < second.timeout {
			t.Errorf("second Acquire returned after %v, want it to have actually polled for at least its %v timeout", elapsed, second.timeout)
		}
	})

	t.Run("expired TTL is acquirable by a new runID", func(t *testing.T) {
		t.Parallel()
		cli, mr := newLockTestClient(t)

		first := &RunLock{cli: cli, key: LockKey, runID: "run-expiring", ttl: 50 * time.Millisecond, timeout: time.Second, pollGap: 10 * time.Millisecond}
		if err := first.Acquire(context.Background()); err != nil {
			t.Fatalf("first Acquire: %v", err)
		}

		mr.FastForward(200 * time.Millisecond) // advance miniredis's clock past the TTL — no real sleep needed

		second := &RunLock{cli: cli, key: LockKey, runID: "run-recovering", ttl: time.Minute, timeout: time.Second, pollGap: 10 * time.Millisecond}
		start := time.Now()
		if err := second.Acquire(context.Background()); err != nil {
			t.Fatalf("second Acquire after TTL expiry: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Errorf("second Acquire took %v after the TTL already expired — it should win on its first SET NX, not poll", elapsed)
		}

		got, err := cli.Get(context.Background(), LockKey).Result()
		if err != nil {
			t.Fatalf("Get(%s): %v", LockKey, err)
		}
		if got != "run-recovering" {
			t.Errorf("lock value after stale-recovery Acquire = %q, want the new holder's runID %q", got, "run-recovering")
		}
	})

	t.Run("Release deletes the key, letting a later Acquire win immediately", func(t *testing.T) {
		t.Parallel()
		cli, _ := newLockTestClient(t)

		l := &RunLock{cli: cli, key: LockKey, runID: "run-releasing", ttl: time.Minute, timeout: time.Second, pollGap: 10 * time.Millisecond}
		if err := l.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if err := l.Release(context.Background()); err != nil {
			t.Fatalf("Release: %v", err)
		}
		if _, err := cli.Get(context.Background(), LockKey).Result(); !errors.Is(err, goredis.Nil) {
			t.Errorf("Get after Release = %v, want redis.Nil (key gone)", err)
		}

		next := &RunLock{cli: cli, key: LockKey, runID: "run-next", ttl: time.Minute, timeout: time.Second, pollGap: 10 * time.Millisecond}
		if err := next.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire after Release: %v", err)
		}
	})
}
