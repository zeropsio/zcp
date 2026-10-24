package ops

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/zeropsio/zcp/internal/topology"
)

// PoolMigrateLeases are the leases zcp holds of pool on branch of the
// repository origin ("" for any), by its own records alone — no orchestrator
// call, no ssh — lowest container number first: what the pool-migrate read
// side derives its lease step from. The retire asks the pool's orchestrator
// whether each is still held, and verifies one, before it relies on it.
func PoolMigrateLeases(stateDir, pool, origin, branch string) ([]LeaseTargetInfo, error) {
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return nil, fmt.Errorf("leases of %s: %w", pool, err)
	}
	var out []leaseRecord
	for _, r := range records {
		if r.PoolHostname != pool || recordBranch(r) != branch {
			continue
		}
		if origin != "" && !topology.SameRepoURL(recordRepo(stateDir, r), origin) {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b leaseRecord) int { return cmp.Compare(a.ContainerNumber, b.ContainerNumber) })
	infos := make([]LeaseTargetInfo, 0, len(out))
	for _, r := range out {
		infos = append(infos, r.targetInfo())
	}
	return infos, nil
}
