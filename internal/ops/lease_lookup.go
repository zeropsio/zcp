package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

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
	// Parked: zcp's record says the lease is parked — its container stopped
	// and kept (DP-28) — so nothing answers on it until an unpark.
	Parked bool
	// StableNumber is the container's ZEROPS_StableNumber as the checkout
	// read it, 0 when the record does not know it.
	StableNumber int
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
		Parked:            r.Parked,
		StableNumber:      r.StableNumber,
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

// LeaseTargetChecked is LeaseTarget for a call that reaches the lease's
// container over ssh or sshfs — a dev server, a verify, a deploy from it, a
// mount, zcp init's remount (docs/spec-workflows.md §8 DP-2): it asks the
// pool's orchestrator, once, with the record's own lease, whether the
// container is still this lease's (readLeaseOwnership). One that is not —
// gone, being deleted, free, another holder's — is refused as lost
// (IsLeaseLost) naming the release that drops its record; one the
// orchestrator does not answer about, or that this zcp cannot ask (no
// orchestrator wired), is refused with why; either way nothing reaches the
// container. what is what the call would have done with it ("verified",
// "deployed from", …). A lease the orchestrator gives zcp is returned parked
// when its record says so or its container is stopped (DP-28). ok is false
// when host is no lease zcp holds.
func LeaseTargetChecked(ctx context.Context, d LeaseDeps, host, what string) (LeaseTargetInfo, bool, error) {
	if d.StateDir == "" || !isLeaseAliasShape(host) {
		return LeaseTargetInfo{}, false, nil
	}
	records, err := readLeaseRecords(d.StateDir)
	if err != nil {
		return LeaseTargetInfo{}, false, err
	}
	for _, rec := range records {
		if rec.alias() != host {
			continue
		}
		info := rec.targetInfo()
		if d.Swarm == nil {
			return info, true, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Lease %s was not %s: this zcp cannot ask the pool's orchestrator whether its container is still this lease's, so it read nothing from it and sent nothing to it", host, what),
				unverifiedRemedy)
		}
		v := readLeaseOwnership(ctx, d, rec)
		switch {
		case v.own.lost():
			refusal := platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Lease %s cannot be %s: %s; zcp does not re-lease it, and nothing reached its container", host, what, v.why),
				"zerops_lease action=release lease="+host+" drops the record")
			refusal.Cause = &leaseLostError{why: v.why}
			return info, true, refusal
		case v.own == leaseUnverified:
			return info, true, unverifiedLeaseRefusal(host, what, v)
		}
		info.Parked = info.Parked || parkedContainer(v.container)
		return info, true, nil
	}
	return LeaseTargetInfo{}, false, nil
}

// leaseLostError is the cause LeaseTargetChecked's refusal of a lost lease
// carries, so a caller tells it apart from one of a lease the orchestrator
// did not answer about (IsLeaseLost).
type leaseLostError struct{ why string }

func (e *leaseLostError) Error() string { return "the lease is lost: " + e.why }

// IsLeaseLost reports whether err is LeaseTargetChecked's refusal of a lease
// that is no longer zcp's — its container gone, being deleted, free or another
// holder's — whose record only its release drops; false for any other error,
// the refusal of a lease the orchestrator did not answer about, or that this
// zcp cannot ask, among them (docs/spec-workflows.md §8 DP-2).
func IsLeaseLost(err error) bool {
	var lost *leaseLostError
	return errors.As(err, &lost)
}

// LeaseGroup is the group of the lease addressed by alias: the leases zcp
// holds of its repository and branch, one per pool — the lease itself on its
// own pool, the earliest acquired on every other — earliest acquired first
// (docs/spec-workflows.md §8 DP-17). It reads zcp's records only, never the
// orchestrator. A record older than keeping its repository is of its pool's
// default; a lease whose repository zcp knows neither way is a group of one.
func LeaseGroup(stateDir, alias string) ([]LeaseTargetInfo, error) {
	return leaseGroupWithout(stateDir, alias, nil)
}

// leaseGroupWithout is LeaseGroup as it will be once the leases in leaving
// (aliases) have left: a rewire computes the overlay of a release's other
// leases before the release drops its record (DP-26).
func leaseGroupWithout(stateDir, alias string, leaving map[string]bool) ([]LeaseTargetInfo, error) {
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
		case leaving[r.alias()] || taken[r.PoolHostname]:
			continue
		default:
			taken[r.PoolHostname] = true
		}
		members = append(members, r.targetInfo())
	}
	return members, nil
}
