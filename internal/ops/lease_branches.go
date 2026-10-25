package ops

import (
	"cmp"
	"slices"
)

// LeaseBranches is every lease zcp holds a record of, by alias, each with its
// repository as a group keys it (recordRepo: an older record's is its pool's
// default, canonical) and its branch (recordBranch: none is main). It reads
// zcp's records only, never the orchestrator, and never carries a lease: what
// a change's targeting and status read to tell which branches the Mate works
// on in which repository.
func LeaseBranches(stateDir string) ([]LeaseTargetInfo, error) {
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return nil, err
	}
	out := make([]LeaseTargetInfo, 0, len(records))
	for _, r := range records {
		info := r.targetInfo()
		info.Repo, info.Branch = recordRepo(stateDir, r), recordBranch(r)
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b LeaseTargetInfo) int { return cmp.Compare(a.Alias, b.Alias) })
	return out, nil
}
