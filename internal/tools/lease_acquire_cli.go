package tools

import (
	"context"
	"errors"
	"fmt"
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
	ID                string `json:"id"`
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
	change, err := ops.LeaseAcquireForMate(ctx, deps, pool, repo, branch)
	if err != nil {
		if reservation.refused(err) {
			return LeaseAcquireAnswer{Refused: personAcquireRefusal(err, pool, branch)}, LeaseAcquireExitRefused
		}
		message, failure := leaseReleaseFailure(err)
		return LeaseAcquireAnswer{Error: message, LeaseReleaseFailure: failure}, LeaseAcquireExitFailed
	}
	v := change.Leases[0] // One view per requested pool.
	return LeaseAcquireAnswer{acquiredLease: &acquiredLease{
		Lease: acquiredLeaseIdentity{ID: v.HolderID, Alias: v.Alias, Pool: v.Pool, Branch: v.Branch, Repo: v.Repo,
			AcquiredAt: v.AcquiredAt, ContainerHostname: v.ContainerHostname},
		Reused: v.Reused,
	}}, LeaseAcquireExitHeld
}

// personAcquireRefusal renders an already classified refusal for the person pressing Apply.
func personAcquireRefusal(err error, pool, branch string) *acquireRefusal {
	_, failure := leaseReleaseFailure(err)
	r := &acquireRefusal{Code: failure.Code}
	var full *ops.LeasePoolFullError
	var held *ops.ConversationLeaseError
	var prereq *ops.LeasePrerequisiteError
	var swarm *platform.SwarmError
	switch {
	case errors.As(err, &full):
		r.Message, r.Suggestion = full.PersonWords()
	case errors.As(err, &held):
		r.Message = fmt.Sprintf("A conversation of this Mate holds branch %s on %s (%s).", branch, pool, held.Alias)
		r.Suggestion = "Archive that conversation, or give the builder another name."
	case errors.As(err, &swarm):
		if swarm.Reason == platform.SwarmReasonContainersPreparing {
			r.Message = fmt.Sprintf("The pool %s is preparing a container.", pool)
			r.Suggestion = "Try Apply again in a minute."
		} else {
			r.Message = fmt.Sprintf("This Mate cannot reach pool %s because its key is missing or was refused.", pool)
			r.Suggestion = "Check the pool's key in Zerops, then try Apply again."
		}
	case r.Code == platform.ErrServiceNotFound:
		r.Message = fmt.Sprintf("There is no pool named %s in this project.", pool)
		r.Suggestion = "Choose an existing pool for the builder and try Apply again."
	case errors.As(err, &prereq):
		r.Message, r.Suggestion = prereq.PersonWords()
	case r.Code == platform.ErrPrerequisiteMissing:
		r.Message = fmt.Sprintf("This Mate cannot use pool %s for branch %s.", pool, branch)
		r.Suggestion = "Check that the pool is deployed and running in Zerops and that this Mate has its key, then try Apply again."
	case r.Code == platform.ErrInvalidParameter:
		r.Message = fmt.Sprintf("The crew cannot use branch %s on pool %s.", branch, pool)
		r.Suggestion = "Check the builder's name and pool, then try Apply again."
	case r.Code == platform.ErrAPITimeout:
		r.Message = fmt.Sprintf("Zerops did not answer in time for pool %s.", pool)
		r.Suggestion = "Try Apply again."
	default:
		r.Message = fmt.Sprintf("This Mate does not have permission to use pool %s.", pool)
		r.Suggestion = "Check this Mate's access to the pool in Zerops, then try Apply again."
	}
	return r
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
	case platform.ErrPermissionDenied, platform.ErrInvalidParameter, platform.ErrServiceNotFound, platform.ErrPrerequisiteMissing, platform.ErrAPITimeout:
		return true
	}
	return false
}
