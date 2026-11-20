package tools

import (
	"context"
	"errors"
	"net/http"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// The exit codes of zcp lease acquire (DP-39).
const (
	LeaseAcquireExitHeld    = 0
	LeaseAcquireExitFailed  = 1
	LeaseAcquireExitRefused = 3
)

// LeaseAcquireAnswer is the crew writer's lease, a refusal that reserved
// nothing, or a failure. Embed the successful answer so reused is present
// even when false, and absent on both kinds of failure.
type LeaseAcquireAnswer struct {
	*acquiredLease
	Refused *acquireRefusal `json:"refused,omitempty"`
	Error   string          `json:"error,omitempty"`
	*LeaseReleaseFailure
}

type acquiredLease struct {
	Lease  acquiredLeaseIdentity `json:"lease"`
	Reused bool                  `json:"reused"`
}

type acquiredLeaseIdentity struct {
	Alias             string `json:"alias"`
	Pool              string `json:"pool"`
	Branch            string `json:"branch"`
	Repo              string `json:"repo"`
	AcquiredAt        string `json:"acquiredAt"`
	ContainerHostname string `json:"containerHostname"`
}

type acquireRefusal struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

// AcquireLeaseAnswer runs the single-pool acquire. Only an MCP call binds a
// session to the answer. The CLI passes the lease identity to the Mate.
func AcquireLeaseAnswer(ctx context.Context, deps ops.LeaseDeps, pool, repo, branch string) (LeaseAcquireAnswer, int) {
	var reservation acquireReservation
	swarm := deps.Swarm
	deps.Swarm = func(url, bearer string) ops.SwarmAPI {
		return acquireAttempt{SwarmAPI: swarm(url, bearer), reservation: &reservation}
	}
	deps.PublicAccess = leasePublicAccess(deps.Client, deps.ProjectID, deps.StateDir)
	change, err := ops.LeaseAcquireGroupChangeFrom(ctx, deps, []string{pool}, repo, branch, "")
	if err != nil {
		message, failure := leaseReleaseFailure(err)
		if reservation.refused(err) {
			return LeaseAcquireAnswer{Refused: &acquireRefusal{
				Code: failure.Code, Message: message, Suggestion: failure.Suggestion,
			}}, LeaseAcquireExitRefused
		}
		return LeaseAcquireAnswer{Error: message, LeaseReleaseFailure: failure}, LeaseAcquireExitFailed
	}
	v := change.Leases[0] // One view per requested pool.
	return LeaseAcquireAnswer{acquiredLease: &acquiredLease{
		Lease: acquiredLeaseIdentity{Alias: v.Alias, Pool: v.Pool, Branch: v.Branch, Repo: v.Repo,
			AcquiredAt: v.AcquiredAt, ContainerHostname: v.ContainerHostname},
		Reused: v.Reused,
	}}, LeaseAcquireExitHeld
}

// Both preflight and a checkout after reservation can fail with the same
// platform code. Observe the single member's reservation call itself. Its
// goroutine joins before the group answers, and warm work never acquires.
type acquireAttempt struct {
	ops.SwarmAPI
	reservation *acquireReservation
}

type acquireReservation struct {
	attempted bool
	declined  bool
}

func (a acquireAttempt) Acquire(ctx context.Context, req platform.SwarmAcquireRequest) (platform.SwarmAcquired, error) {
	got, err := a.SwarmAPI.Acquire(ctx, req)
	var se *platform.SwarmError
	a.reservation.attempted = true
	a.reservation.declined = errors.As(err, &se) &&
		(se.Reason == platform.SwarmReasonAllReserved || se.Reason == platform.SwarmReasonContainersPreparing ||
			se.Status == http.StatusUnauthorized || se.Code == "notAuthorized")
	return got, err
}

func (r acquireReservation) refused(err error) bool {
	if r.attempted {
		return r.declined
	}
	// Only zcp's preflight refusals qualify. A failed read of state or
	// the platform and a failed checkout of a reused lease stay failures.
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Cause != nil {
		return false
	}
	switch pe.Code {
	case platform.ErrInvalidParameter, platform.ErrServiceNotFound, platform.ErrPrerequisiteMissing, platform.ErrAPITimeout:
		return true
	}
	return false
}
