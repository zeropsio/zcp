package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// DeployGateError is the typed error returned by DeployLocal / DeploySSH
// when the pre-flight diagnose-before-destruct gate refuses the deploy.
// Carries the Recovery hint so the tools layer's convertError can attach
// it to the ErrorWire response. Errors.As-friendly.
type DeployGateError struct {
	*platform.PlatformError
	Recovery *topology.Recovery
}

// Unwrap exposes the embedded PlatformError so errors.As(err, &pe)
// continues to work after the gate wrap.
func (e *DeployGateError) Unwrap() error {
	return e.PlatformError
}

// NewDeployGateError builds the DeployGateError for the given target +
// Recovery. The error message names the refused hostname and the status
// that triggered the gate; the Suggestion points at the Recovery target.
func NewDeployGateError(target *platform.ServiceStack, rec *topology.Recovery) error {
	hostname := ""
	status := ""
	if target != nil {
		hostname = target.Name
		status = target.Status
	}
	pe := platform.NewPlatformError(
		platform.ErrDiagnosisRequired,
		fmt.Sprintf("deploy refused: %q is in %s; diagnose the previous failure before redeploying", hostname, status),
		fmt.Sprintf("Call %s with the recovery args, identify the failure cause, then re-deploy.", rec.Tool),
	)
	return &DeployGateError{PlatformError: pe, Recovery: rec}
}

// GateNonRunningOnDeploy returns a Recovery hint when a deploy attempt
// targets a service in a non-running terminal state that signals "diagnose
// before redeploying." Used by deploy_local + deploy_ssh as a pre-flight
// guard so the agent doesn't burn a build cycle (or destroy more state) on
// a service whose previous deploy already failed.
//
// Gate semantics (matches the discriminator in NonRunningRecovery so the
// hint shape is consistent across surfaces):
//
//	RUNNING / ACTIVE → nil (healthy; let through)
//	READY_TO_DEPLOY + LatestFailedAppVersionContext == nil → nil
//	    (first-deploy normal path; no diagnostic data to read yet)
//	READY_TO_DEPLOY + classified failed history → events Recovery (read-first)
//	FAILED → events Recovery
//	STOPPED / NEW / other → nil (intentional state — downstream layer
//	    surfaces its own error if deploy can't proceed)
//
// DELIBERATELY NARROW vs the import-override gate: this gate keys on a
// CLASSIFIED failed appVersion (LatestFailedAppVersionContext), NOT the broader
// HasPriorDeployAttempt the destructive import-override gate uses. The two
// protect different things — override is DESTRUCTIVE (wipes code), so it gates
// on ANY prior history; a deploy is CORRECTIVE + non-destructive, and the
// service stays READY_TO_DEPLOY+history until a deploy SUCCEEDS, so broadening
// here would block the very recovery deploy the agent issues after diagnosing
// (recover-failed: add missing dep → redeploy). The asymmetry is intentional;
// do not "unify" the two gates' predicates (Codex review 2026-06-03 flagged
// the inconsistency; rejected — broadening blocks corrective redeploys).
//
// Returns (nil, err) only when the platform call inside
// LatestFailedAppVersionContext fails. (nil, nil) is the gate-passes
// sentinel; (Recovery, nil) is the gate-fires sentinel — caller wraps
// it into the user-facing error.
func GateNonRunningOnDeploy(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID string,
	target *platform.ServiceStack,
) (*topology.Recovery, error) {
	if target == nil {
		return nil, nil //nolint:nilnil // gate-passes sentinel: nothing to inspect
	}
	switch target.Status {
	case platform.ServiceStatusRunning, platform.ServiceStatusActive:
		return nil, nil //nolint:nilnil // healthy
	case platform.ServiceStatusFailed:
		return NonRunningRecovery(ctx, client, fetcher, projectID, target.Name, target.Status), nil
	case platform.ServiceStatusReadyToDeploy:
		failed, err := LatestFailedAppVersionContext(ctx, client, fetcher, projectID, target.Name)
		if err != nil {
			return nil, err
		}
		if failed == nil {
			return nil, nil //nolint:nilnil // first-deploy normal path
		}
		return NonRunningRecovery(ctx, client, fetcher, projectID, target.Name, target.Status), nil
	}
	return nil, nil //nolint:nilnil // out-of-scope state; downstream surfaces its own error
}
