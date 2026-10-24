package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A pair whose dev half is a dev pool (docs/spec-workflows.md §13 PM-10, §8
// E8) has no push-source container: the pool's hostname reaches a random one
// of its containers, possibly someone's lease (§8 PS-3). The delivery and
// launch readers ask DevIsPool before any container work on the meta's
// hostname, and answer with the refusals below — before any read.

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

// poolDevPairLaunchBlocker is launch's refusal of a part whose pair is keyed
// by the dev pool named pool (docs/spec-workflows.md §10): launch reads the
// pushed source, zerops.yaml and origin in the push source's checkout, and a
// pool has none. Reading a migrated part from its origin is a follow-up; the
// refusal comes before any read.
func poolDevPairLaunchBlocker(pool string, stages []string) topology.Blocker {
	part := "its stage"
	if len(stages) > 0 {
		part = "stage " + strings.Join(stages, ", ")
	}
	return topology.Blocker{
		ID:       "pool-dev-pair-" + pool,
		Severity: topology.BlockerSeverityBlock,
		Category: topology.BlockerCategorySourceControl,
		Message: fmt.Sprintf(
			"Launch refused: the pair of %s is keyed by its dev pool %s. A pool holds no checkout of its own — its hostname reaches a random one of its containers, possibly someone's lease — so there is no push source to read the pushed code, zerops.yaml or origin from. Launching a part moved onto a dev pool is not supported yet; nothing was read or changed.",
			part, pool),
	}
}

// poolDevPairLaunchBlockers are the blockers of the runtimes a launch names
// whose pair is keyed by its dev pool, one per pool.
func poolDevPairLaunchBlockers(stateDir string, runtimes []resolvedLaunchRuntime) []topology.Blocker {
	var out []topology.Blocker
	seen := map[string]bool{}
	for _, r := range runtimes {
		if !r.DevIsPool || seen[r.PushHostname] {
			continue
		}
		seen[r.PushHostname] = true
		var stages []string
		if meta, _ := workflow.FindServiceMeta(stateDir, r.PushHostname); meta != nil {
			stages = meta.StageHostnames()
		}
		out = append(out, poolDevPairLaunchBlocker(r.PushHostname, stages))
	}
	return out
}
