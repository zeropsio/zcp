package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// failedContainer is a container of the pool a platform action failed on: no
// lease holds it and no acquire takes it, yet it counts toward the pool's
// maximum (live, 2026-10-25: webpool #5 stood ACTION_FAILED after a restart
// its exhausted process table could not run, and every acquire after was
// refused "full: 2 of at most 3 containers are leased").
func failedContainer(id string, number int64, status, lock string) platform.SwarmContainer {
	return platform.SwarmContainer{ID: id, Number: number, Hostname: nodeHostname(number), Status: status, ServiceID: "svc-pool", LockState: lock}
}

// TestLeaseAcquire_FullPool_NamesTheFailedContainerItCounts: a full pool's
// refusal names each container that holds one of its places though no
// acquire takes it — which one, its state — says zcp cannot remove it, and
// names the way out, the person's call; a container being removed is named
// as one that frees its place in a moment.
func TestLeaseAcquire_FullPool_NamesTheFailedContainerItCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		containers  []platform.SwarmContainer
		wantMessage []string
		wantSuggest []string
		banned      []string
	}{
		{
			name: "a failed container",
			containers: []platform.SwarmContainer{
				reservedContainer("c-3", 3), reservedContainer("c-4", 4), failedContainer("c-5", 5, "ACTION_FAILED", "free"),
			},
			wantMessage: []string{"2 of at most 3 containers are leased", "devpool-5", "ACTION_FAILED"},
			wantSuggest: []string{"devpool-5", "cannot remove it", "zerops_events serviceHostname=\"devpool\"", "ask the person", "zerops_scale serviceHostname=\"devpool\" maxContainers=4"},
		},
		{
			name: "a container being removed",
			containers: []platform.SwarmContainer{
				reservedContainer("c-3", 3), reservedContainer("c-4", 4), failedContainer("c-5", 5, "ACTIVE", "deleting"),
			},
			wantMessage: []string{"2 of at most 3 containers are leased", "devpool-5", "being removed"},
			wantSuggest: []string{"devpool-5", "acquire again"},
			banned:      []string{"cannot remove it"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 3, "b3")
			recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 4, "b4")
			fx.swarm.containers = tt.containers
			fx.swarm.limits = platform.SwarmPool{MaximumContainerCount: 3}
			fx.swarm.acquireErr = allReserved(causeAtMaximum)

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "build-stamp")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a *PlatformError", err)
			}
			for _, want := range tt.wantMessage {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q does not say %q", pe.Message, want)
				}
			}
			for _, want := range tt.wantSuggest {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q does not say %q", pe.Suggestion, want)
				}
			}
			for _, b := range tt.banned {
				if strings.Contains(pe.Message+pe.Suggestion, b) {
					t.Errorf("refusal says %q: %s / %s", b, pe.Message, pe.Suggestion)
				}
			}
		})
	}
}
