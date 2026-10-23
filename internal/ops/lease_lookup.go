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
			return LeaseTargetInfo{
				Alias:             host,
				Pool:              r.PoolHostname,
				PoolServiceID:     r.PoolServiceID,
				ContainerHostname: r.ContainerHostname,
				Branch:            r.Branch,
			}, true, nil
		}
	}
	return LeaseTargetInfo{}, false, nil
}
