package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// LeaseRolloutView is what a rollout of a pool did, as the agent sees it
// (docs/spec-workflows.md §8 DP-7).
type LeaseRolloutView struct {
	Pool string `json:"pool"`
	// ActiveAppVersionID is the app version every created container runs.
	ActiveAppVersionID string `json:"activeAppVersionId,omitempty"`
	Replaced           int    `json:"replaced"`
	Created            int    `json:"created"`
	Retiring           int    `json:"retiring"`
	Skipped            int    `json:"skipped"`
	// Skips are the skipped containers, each with its reason.
	Skips []LeaseRolloutSkip `json:"skips"`
	// CreateErrors say why replacements could not be created.
	CreateErrors []string `json:"createErrors,omitempty"`
	Note         string   `json:"note"`
}

// LeaseRolloutSkip is an outdated container the rollout did not replace.
type LeaseRolloutSkip struct {
	ContainerID string `json:"containerId"`
	// Lease is the alias, when the container is a lease zcp holds.
	Lease   string `json:"lease,omitempty"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// LeaseRollout replaces the pool's outdated free containers with ones built
// for its current app version, through its orchestrator. It never forces:
// every reserved container — each lease, zcp's own included — is skipped as
// held-by-other and keeps its version until it is released and the pool is
// rolled out again; force needs the admin token and would take a lease from
// its holder.
func LeaseRollout(ctx context.Context, d LeaseDeps, pool string) (LeaseRolloutView, error) {
	rp, err := resolvePool(ctx, d, pool)
	if err != nil {
		return LeaseRolloutView{}, err
	}
	got, err := rp.api.Rollout(ctx)
	if err != nil {
		return LeaseRolloutView{}, rolloutError(pool, err)
	}
	return rolloutView(pool, got, ownLeaseAliases(d.StateDir, pool, rp.svc.ID)), nil
}

// rolloutError is a failed rollout as the agent reads it. A pool whose
// minimum container count equals its maximum has no room to replace anything
// (matched on the orchestrator's reason, never its message).
func rolloutError(pool string, err error) error {
	var se *platform.SwarmError
	if !errors.As(err, &se) || se.Reason != platform.SwarmReasonPoolFixedSize {
		return leaseOrchestratorError(pool, "roll out", err)
	}
	out := platform.NewPlatformError(platform.ErrAPIError,
		fmt.Sprintf("Pool '%s' cannot be rolled out: its minimum container count equals its maximum, and a rollout needs room between them to replace a container", pool),
		fmt.Sprintf("Make room, then roll out again: raise the pool's maximum or lower its minimum (zerops_scale serviceHostname=%q maxContainers=... or minContainers=...)", pool))
	out.APICode = se.Reason
	out.Cause = err
	return out
}

// ownLeaseAliases maps the container id of each lease zcp holds on the pool
// to its alias. An unreadable record set names none: the rollout's answer is
// still the truth.
func ownLeaseAliases(stateDir, pool, poolServiceID string) map[string]string {
	records, err := readLeaseRecords(stateDir)
	if err != nil {
		return nil
	}
	aliases := map[string]string{}
	for _, r := range records {
		if r.PoolHostname == pool && r.PoolServiceID == poolServiceID {
			aliases[r.ContainerID] = r.alias()
		}
	}
	return aliases
}

// rolloutView is the orchestrator's answer as the agent reads it.
func rolloutView(pool string, got platform.SwarmRollout, aliases map[string]string) LeaseRolloutView {
	v := LeaseRolloutView{
		Pool: pool, ActiveAppVersionID: got.ActiveAppVersionID,
		Replaced: len(got.Replaced), Created: len(got.Created), Retiring: len(got.Retiring), Skipped: len(got.Skipped),
		Skips: make([]LeaseRolloutSkip, 0, len(got.Skipped)),
	}
	for _, s := range got.Skipped {
		v.Skips = append(v.Skips, LeaseRolloutSkip{ContainerID: s.ID, Lease: aliases[s.ID], Reason: s.Reason, Message: s.Message})
	}
	for _, ce := range got.CreateErrors {
		name := ce.ContainerName
		if name == "" {
			name = ce.ContainerID
		}
		if name != "" {
			v.CreateErrors = append(v.CreateErrors, name+": "+ce.Message)
			continue
		}
		v.CreateErrors = append(v.CreateErrors, ce.Message)
	}
	notes := []string{"Leases (reserved containers) are skipped as held-by-other and keep their version until they are released and the pool is rolled out again; zcp never forces a rollout."}
	if v.Retiring > 0 {
		notes = append(notes, fmt.Sprintf("%d container(s) with work in progress retire: the orchestrator replaces each once its work ends.", v.Retiring))
	}
	v.Note = strings.Join(notes, " ")
	return v
}
