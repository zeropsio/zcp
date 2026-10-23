package ops

// LeaseTargetInfo names the container a lease alias reaches: what deploy, dev
// servers and verify need to address a lease instead of a service. It never
// carries the lease string.
type LeaseTargetInfo struct {
	Alias             string
	Pool              string
	PoolServiceID     string
	ContainerHostname string
	Branch            string
	// Repo is the canonical repository the lease checked out, "" when its
	// record predates keeping it.
	Repo string
}

// targetInfo is the record as a lease target: never the lease.
func (r leaseRecord) targetInfo() LeaseTargetInfo {
	return LeaseTargetInfo{
		Alias:             r.alias(),
		Pool:              r.PoolHostname,
		PoolServiceID:     r.PoolServiceID,
		ContainerHostname: r.ContainerHostname,
		Branch:            r.Branch,
		Repo:              r.Repo,
	}
}

// LeaseTarget reports whether host is the alias of a lease zcp holds (a
// record in <stateDir>/leases), and which pool and container it reaches.
// A host with the alias shape but no record is not a lease: only zcp's own
// records make an alias addressable (DP-1, DP-2).
func LeaseTarget(stateDir, host string) (LeaseTargetInfo, bool, error) {
	if stateDir == "" || !isLeaseAliasShape(host) {
		return LeaseTargetInfo{}, false, nil
	}
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return LeaseTargetInfo{}, false, err
	}
	for _, r := range records {
		if r.alias() == host {
			return r.targetInfo(), true, nil
		}
	}
	return LeaseTargetInfo{}, false, nil
}

// LeaseGroup is the group of the lease addressed by alias: the leases zcp
// holds of its repository and branch, one per pool — the lease itself on its
// own pool, the earliest acquired on every other — earliest acquired first
// (docs/spec-workflows.md §8 DP-17). It reads zcp's records only, never the
// orchestrator. A record older than keeping its repository is of its pool's
// default; a lease whose repository zcp knows neither way is a group of one.
func LeaseGroup(stateDir, alias string) ([]LeaseTargetInfo, error) {
	rec, err := loadLeaseRecord(stateDir, alias)
	if err != nil {
		return nil, err
	}
	repo := recordRepo(stateDir, rec)
	if repo == "" {
		return []LeaseTargetInfo{rec.targetInfo()}, nil
	}
	group, err := leaseGroupRecords(stateDir, repo, recordBranch(rec))
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{rec.PoolHostname: true}
	members := make([]LeaseTargetInfo, 0, len(group))
	for _, r := range group {
		switch {
		case r.alias() == alias:
		case taken[r.PoolHostname]:
			continue
		default:
			taken[r.PoolHostname] = true
		}
		members = append(members, r.targetInfo())
	}
	return members, nil
}
