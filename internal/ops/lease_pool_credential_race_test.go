// Tests for: ops/lease_pool_credential.go — the per-pool decision to write the
// HQ credential, and the write, run one at a time per pool
// (docs/spec-workflows.md §8 DP-16).
package ops

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// envBarrierClient holds every read of a service's env until n readers wait
// there at once, or a bound passes: callers that race reach the write
// together, callers serialised by a lock wait out the bound one by one.
type envBarrierClient struct {
	*platform.Mock
	n       int
	bound   time.Duration
	mu      sync.Mutex
	arrived int
	all     chan struct{}
}

func newEnvBarrierClient(mock *platform.Mock, n int) *envBarrierClient {
	return &envBarrierClient{Mock: mock, n: n, bound: 300 * time.Millisecond, all: make(chan struct{})}
}

func (c *envBarrierClient) GetServiceEnv(ctx context.Context, serviceID string) ([]platform.ServiceEnvVar, error) {
	c.mu.Lock()
	c.arrived++
	if c.arrived == c.n {
		close(c.all)
	}
	c.mu.Unlock()
	select {
	case <-c.all:
	case <-time.After(c.bound):
	}
	return c.Mock.GetServiceEnv(ctx, serviceID)
}

// TestEnsurePoolHQCredential_ConcurrentAcquiresOfOnePool_WriteOnce: acquires of
// different branches on one pool hold different key locks, so they reach the
// credential decision together. Each write deletes the variable and creates
// it again, and in between every fresh session of the pool reads none — so
// one pool's decision and write run one at a time, and the callers that
// waited find the credential written.
func TestEnsurePoolHQCredential_ConcurrentAcquiresOfOnePool_WriteOnce(t *testing.T) {
	t.Parallel()
	const acquires = 4
	fx := newLeaseFixture(t)
	inAMate(fx)
	client := newEnvBarrierClient(fx.deps.Client.(*platform.Mock), acquires)
	fx.deps.Client = client
	pool := &platform.ServiceStack{ID: "svc-pool", Name: testPoolHost}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, acquires)
	for range acquires {
		wg.Go(func() {
			<-start
			_, err := ensurePoolHQCredential(context.Background(), fx.deps, pool, leaseHQRepo)
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("ensurePoolHQCredential: %v", err)
		}
	}
	if got := client.CallCount("CreateServiceEnvVar"); got != 1 {
		t.Errorf("HQ credential creates = %d, want 1: concurrent acquires of one pool each wrote it", got)
	}
	if got := client.CallCount("DeleteUserData"); got != 0 {
		t.Errorf("HQ credential deletes = %d, want 0: a write deleted the variable another acquire had just created", got)
	}
}

// TestRenewPoolHQCredentials_RacingAnAcquire_WritesOnce: the repository pass
// renewing a pool after the Mate enrolled again and an acquire on that pool
// both see the old fingerprint; whichever writes first, the other re-reads
// the record under the pool's lock and leaves the credential alone.
func TestRenewPoolHQCredentials_RacingAnAcquire_WritesOnce(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	inAMate(fx)
	client := newEnvBarrierClient(fx.deps.Client.(*platform.Mock), 2)
	fx.deps.Client = client
	writePoolCredentialRecord(t, fx.stateDir, testPoolHost, "svc-pool", hq.GitCredentialFingerprint(poolOldCredential))
	pool := &platform.ServiceStack{ID: "svc-pool", Name: testPoolHost}

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		if _, err := ensurePoolHQCredential(context.Background(), fx.deps, pool, leaseHQRepo); err != nil {
			t.Errorf("ensurePoolHQCredential: %v", err)
		}
	})
	wg.Go(func() {
		<-start
		if failures := RenewPoolHQCredentials(context.Background(), client, fx.stateDir, poolMateCredential); len(failures) != 0 {
			t.Errorf("RenewPoolHQCredentials failures = %+v", failures)
		}
	})
	close(start)
	wg.Wait()
	if got := client.CallCount("CreateServiceEnvVar"); got != 1 {
		t.Errorf("HQ credential creates = %d, want 1: the renewal and the acquire each wrote it", got)
	}
}
