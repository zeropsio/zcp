package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// NonRunningRecovery returns the canonical Recovery hint for a service in a
// non-running terminal state, or nil when the status is intentional / pre-
// deploy / healthy. Thin adapter over ComputeRecoveryState (docs/spec-
// workflows.md §8 "Recovery classification" R1: one classification, many
// readers — this function never re-derives a recovery branch from
// service.status on its own).
//
// fetcher is currently unused (the classifier's default log enrichment is
// unnecessary for a recovery-hint verdict) but retained in the signature so
// callers don't have to change.
func NonRunningRecovery(
	ctx context.Context,
	client platform.Client,
	_ platform.LogFetcher,
	projectID, hostname, status string,
) *topology.Recovery {
	state, err := ComputeRecoveryState(ctx, client, nil, projectID, hostname, status)
	if err != nil {
		return nil
	}
	return state.Next
}
