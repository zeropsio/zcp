package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A pair whose dev half is a dev pool (docs/spec-workflows.md §13 PM-10, §8
// E8) has no push-source container: the pool's hostname reaches a random one
// of its containers, possibly someone's lease (§8 PS-3). The delivery readers
// ask DevIsPool before any container work on the meta's hostname, and answer
// with the refusals below — before any read. Launch and export read such a
// part from its origin instead (pool_part_origin.go, §10 P-LP-17, §9 E10).

// poolPairLeaseAcquire is the call that checks branch out in a lease of
// meta's pool.
func poolPairLeaseAcquire(meta *workflow.ServiceMeta, branch string) string {
	return fmt.Sprintf("zerops_lease action=\"acquire\" pool=%q branch=%q", meta.Hostname, branch)
}

// poolPairRefusal is what a delivery action answers for either hostname of
// a pair keyed by its dev pool, before any ssh call: what did not run and
// why, and how — the hint — it is done from a lease of the pool.
func poolPairRefusal(what, service string, meta *workflow.ServiceMeta, hint string) *platform.PlatformError {
	return platform.NewPlatformError(platform.ErrInvalidUsage,
		fmt.Sprintf("%s did not run: %q belongs to the pair keyed by the dev pool %q, which holds no checkout of its own — its hostname reaches a random one of its containers.", what, service, meta.Hostname),
		hint)
}

// poolPartNoStageBlocker is launch's refusal of a pair keyed by the dev pool
// named pool that has no stage (docs/spec-workflows.md §10 P-LP-17): launch
// promotes a pool-keyed part's stage, and a pool is no app runtime (§8
// PS-3), so the pair holds nothing to promote. It comes before any read.
func poolPartNoStageBlocker(pool string) topology.Blocker {
	return topology.Blocker{
		ID:       "pool-dev-pair-no-stage-" + pool,
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategorySourceControl,
		Message: fmt.Sprintf(
			"Launch refused: the pair keyed by the dev pool %s has no stage. Launch promotes a pool-keyed part's stage — its type, scale and env — built from the part's origin, and a pool is no app runtime, so there is nothing in the pair to promote; nothing was read or changed.",
			pool),
	}
}
