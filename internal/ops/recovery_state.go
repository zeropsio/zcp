package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// RecoveryState is the single classification a service's current state
// yields (docs/spec-workflows.md §8 "Recovery classification" R1).
// ComputeRecoveryState is the ONE producer; every recovery reader — verify/
// provision Recovery pointers (NonRunningRecovery), the zerops_import
// override gate, atom selection — consumes it rather than re-deriving a
// recovery branch from service.status alone.
type RecoveryState struct {
	// Shape is the recovery classification (R1).
	Shape topology.RecoveryShape
	// FailureClass is the classifier's coarse category, set when Shape is
	// a failed-* or stuck-building shape with a classified cause.
	FailureClass topology.FailureClass
	// Cause is the classifier's one-sentence diagnosis, from
	// LatestFailedAppVersionContext when present.
	Cause string
	// Next is the one read-only next call: zerops_events for failed-*/
	// stuck-building, zerops_logs since=15m for fresh-misconfigured, nil
	// for healthy.
	Next *topology.Recovery
	// LiveProcess is the processId of a live build/deploy process
	// referencing the service (busy truth — CLAUDE.md trap), or "" when
	// none. A live process means Shape is never a failed-* shape: busy is
	// not failure.
	LiveProcess string

	// LastAppVersionID is the newest appVersion's ID (by Sequence), read
	// via the DIRECT ListServiceAppVersions — only set for a failed-*/
	// stuck-building Shape (R2). Empty when the service has no app-version
	// history, or for healthy/fresh-misconfigured shapes.
	LastAppVersionID string
	// ArtifactBuilt is true when the newest appVersion's status is
	// DEPLOY_FAILED — the build FINISHED and produced a real artifact, only
	// the deploy/init phase failed. This is the never-activated
	// buildFromGit shape R2 names: the artifact can be redeployed in place
	// via `zerops_deploy appVersion=latest` without a rebuild.
	ArtifactBuilt bool
	// HasContainer is true when the service is currently live (RUNNING/
	// ACTIVE) or carries a non-nil ActiveAppVersion — i.e. a container
	// exists to source an SSH self-deploy from. False is what makes the
	// artifact-redeploy / re-import-with-override recovery the ONLY paths
	// (no in-place rebuild exists on the platform for a container-less
	// service).
	HasContainer bool
	// GitProvisioned is true when the newest appVersion's Source is "GIT"
	// or it carries a PublicGitSource — the service was imported with
	// buildFromGit, so a failed build with no container can be recovered
	// by fixing the cause and re-importing with override=true (nothing
	// deployed is ever lost: no version was ever activated).
	GitProvisioned bool
	// Then is the ONE corrective string R2 names for the shape + facts
	// above — consumed by the zerops_import override gate payload and by
	// NonRunningRecovery's text, and nothing else composes its own. Empty
	// for healthy/fresh-misconfigured (those readers build their own
	// corrective already).
	Then string
}

// eventsRecovery is the read-first recovery pointer shared by every
// failed-*/stuck-building shape (R2: read the failure timeline before any
// reset — never an auto-destructive override).
func eventsRecovery(hostname string) *topology.Recovery {
	return &topology.Recovery{
		Tool:   "zerops_events",
		Action: "fetch",
		Args: map[string]string{
			"serviceHostname": hostname,
		},
	}
}

// logsRecovery is the never-deployed recovery pointer for fresh-
// misconfigured: no diagnostic history exists yet, so logs (not events)
// are the first read.
func logsRecovery(hostname string) *topology.Recovery {
	return &topology.Recovery{
		Tool:   "zerops_logs",
		Action: "fetch",
		Args: map[string]string{
			"serviceHostname": hostname,
			"since":           "15m",
		},
	}
}

// ComputeRecoveryState is the single classification a service's status
// yields (R1). status is the caller's best-known platform.ServiceStatus*
// value; an unresolvable/unknown status (e.g. a failed service lookup)
// falls to the same conservative not-proven-healthy path as
// READY_TO_DEPLOY rather than being assumed fine — a destructive gate must
// never fail open on unknown state.
//
// Order of evaluation: a LIVE build/deploy process is checked FIRST and
// wins over any classified failure — busy is not failure (CLAUDE.md trap:
// "Service busy = a LIVE process ... the SOLE busy-truth"). Only once no
// live process is found does status/history classification run.
func ComputeRecoveryState(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID, hostname, status string,
) (RecoveryState, error) {
	state, err := computeRecoveryStateShape(ctx, client, fetcher, projectID, hostname, status)
	if err != nil {
		return RecoveryState{}, err
	}
	return attachArtifactFacts(ctx, client, projectID, hostname, state), nil
}

// computeRecoveryStateShape is the R1 shape classifier — split out of
// ComputeRecoveryState so the R2 artifact/container facts (attached by
// attachArtifactFacts) run as a single post-processing step regardless of
// which branch below produced the shape.
func computeRecoveryStateShape(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID, hostname, status string,
) (RecoveryState, error) {
	liveProcessID, err := liveProcessForHostname(ctx, client, projectID, hostname)
	if err != nil {
		return RecoveryState{}, fmt.Errorf("compute recovery state: %w", err)
	}
	if liveProcessID != "" {
		return RecoveryState{Shape: topology.RecoveryHealthy, LiveProcess: liveProcessID}, nil
	}

	switch status {
	case platform.ServiceStatusRunning, platform.ServiceStatusActive:
		return RecoveryState{Shape: topology.RecoveryHealthy}, nil
	case platform.ServiceStatusStopped, platform.ServiceStatusNew:
		// Intentional / pre-deploy state — no recovery candidate
		// regardless of any prior deploy history.
		return RecoveryState{Shape: topology.RecoveryHealthy}, nil
	case platform.ServiceStatusFailed:
		return classifiedFailureState(ctx, client, fetcher, projectID, hostname, topology.RecoveryFailedInit)
	case platform.ServiceStatusReadyToDeploy:
		prior, priorErr := HasPriorDeployAttempt(ctx, client, projectID, hostname)
		if priorErr != nil {
			return RecoveryState{}, fmt.Errorf("compute recovery state: %w", priorErr)
		}
		if !prior {
			return RecoveryState{Shape: topology.RecoveryFreshMisconfigured, Next: logsRecovery(hostname)}, nil
		}
		return classifiedFailureState(ctx, client, fetcher, projectID, hostname, topology.RecoveryStuckBuilding)
	default:
		// Unknown/unresolved status (e.g. the caller's service lookup
		// failed) — never assume healthy. Same conservative path as
		// READY_TO_DEPLOY-with-history.
		return classifiedFailureState(ctx, client, fetcher, projectID, hostname, topology.RecoveryStuckBuilding)
	}
}

// classifiedFailureState runs the failure classifier and maps its verdict
// to a failed-* shape, falling back to fallbackShape when the classifier
// found no recognized failure (still gated: the caller reached here
// because the service is neither healthy nor fresh-misconfigured — read
// zerops_events first regardless of whether a class could be attached).
func classifiedFailureState(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID, hostname string,
	fallbackShape topology.RecoveryShape,
) (RecoveryState, error) {
	failed, err := LatestFailedAppVersionContext(ctx, client, fetcher, projectID, hostname)
	if err != nil {
		return RecoveryState{}, fmt.Errorf("compute recovery state: %w", err)
	}
	if failed == nil {
		return RecoveryState{Shape: fallbackShape, Next: eventsRecovery(hostname)}, nil
	}

	shape := topology.RecoveryFailedInit
	switch failed.FailureClass {
	case topology.FailureClassBuild:
		shape = topology.RecoveryFailedBuild
		// The WAITING_TO_BUILD+FAILED-process P2 fallback never sets
		// FailedAt (only the directly classified appVersion path does) —
		// that fallback is the stuck-building shape, not a plain
		// failed-build (docs/spec-workflows.md §8 R1).
		if failed.FailedAt.IsZero() {
			shape = topology.RecoveryStuckBuilding
		}
	case topology.FailureClassStart,
		topology.FailureClassVerify,
		topology.FailureClassNetwork,
		topology.FailureClassConfig,
		topology.FailureClassCredential,
		topology.FailureClassOther:
		// LatestFailedAppVersionContext only ever classifies via the
		// build/prepare/init phases reachable from an appVersion status
		// (FailurePhaseFromStatus); prepare/init both map to
		// FailureClassStart. Every non-build class here collapses to
		// failed-init (the shape's default) — build is the only class
		// this classifier path produces that needs its own shape.
	}
	return RecoveryState{
		Shape:        shape,
		FailureClass: failed.FailureClass,
		Cause:        failed.FailureCause,
		Next:         eventsRecovery(hostname),
	}, nil
}

// appVersionSourceGit is the appVersion Source value stamped on a
// buildFromGit import (docs/spec-workflows.md §8 R2).
const appVersionSourceGit = "GIT"

// attachArtifactFacts fills the R2 artifact/container facts (LastAppVersionID/
// ArtifactBuilt/HasContainer/GitProvisioned) and derives Then — but only for
// a failed-*/stuck-building Shape; healthy and fresh-misconfigured already
// carry their own corrective elsewhere (import.go's fresh-misconfigured
// branch builds its own Retry) and get no Then here.
//
// Best-effort: a lookup/list failure leaves the facts at their zero values
// rather than erroring the whole classification — Then still derives (to
// the generic "every other failed shape" corrective) so a transient
// enrichment failure never blanks out the already-computed Shape/Next.
func attachArtifactFacts(
	ctx context.Context,
	client platform.Client,
	projectID, hostname string,
	state RecoveryState,
) RecoveryState {
	switch state.Shape {
	case topology.RecoveryHealthy, topology.RecoveryFreshMisconfigured:
		return state
	case topology.RecoveryFailedInit, topology.RecoveryFailedBuild, topology.RecoveryStuckBuilding:
	}

	if svc, err := LookupService(ctx, client, projectID, hostname); err == nil && svc != nil {
		state.HasContainer = svc.IsLive() || svc.ActiveAppVersion != nil

		if versions, vErr := client.ListServiceAppVersions(ctx, svc.ID); vErr == nil && len(versions) > 0 {
			newest := versions[0]
			for _, v := range versions[1:] {
				if v.Sequence > newest.Sequence {
					newest = v
				}
			}
			state.LastAppVersionID = newest.ID
			state.ArtifactBuilt = newest.Status == platform.BuildStatusDeployFailed
			state.GitProvisioned = newest.Source == appVersionSourceGit || newest.PublicGitSource != nil
		}
	}

	state.Then = deriveRecoveryThen(state, hostname)
	return state
}

// deriveRecoveryThen is the SINGLE producer of R2's "then" corrective text
// for a failed-*/stuck-building shape — consumed by the zerops_import
// override gate payload and NonRunningRecovery's text (nothing else
// composes its own, per docs/spec-workflows.md §8 R2):
//
//   - failed-init with a built artifact and no container (a never-
//     activated buildFromGit service) ⇒ the in-place appVersion redeploy —
//     no rebuild, no re-import.
//   - failed-build/stuck-building on a git-provisioned service with no
//     container ⇒ fix the cause then re-import with override=true —
//     nothing deployed is ever lost (no version was ever activated); the
//     platform has no in-place git rebuild route.
//   - every other failed-*/stuck-building shape (a container exists) ⇒
//     today's plain, never-gated zerops_deploy — the prior appVersion
//     keeps serving in the meantime.
func deriveRecoveryThen(state RecoveryState, hostname string) string {
	switch {
	case state.Shape == topology.RecoveryFailedInit && state.ArtifactBuilt && !state.HasContainer:
		return fmt.Sprintf(
			"zerops_deploy targetService=%s appVersion=latest (re-deploys the built artifact; no rebuild, no re-import)",
			hostname,
		)
	case (state.Shape == topology.RecoveryFailedBuild || state.Shape == topology.RecoveryStuckBuilding) &&
		state.GitProvisioned && !state.HasContainer:
		return "fix the cause, then re-import the same buildFromGit entry with override=true — " +
			"nothing deployed is lost (no version ever activated); the platform has no in-place git rebuild"
	default:
		return fmt.Sprintf(
			"zerops_deploy targetService=%s (never gated; the prior appVersion keeps serving)",
			hostname,
		)
	}
}

// liveProcessForHostname returns the processId of a live (PENDING/RUNNING/
// ROLLBACKING/CANCELING) process referencing hostname's service, or "" when
// none. Reuses ProjectActivity (the single busy-truth owner) rather than a
// second ad hoc SearchProcesses scan.
func liveProcessForHostname(ctx context.Context, client platform.Client, projectID, hostname string) (string, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return "", err
	}
	idToHost := make(map[string]string, len(services))
	for _, s := range services {
		idToHost[s.ID] = s.Name
	}
	activity, err := ProjectActivity(ctx, client, projectID, idToHost)
	if err != nil {
		return "", err
	}
	ops := activity[hostname]
	if len(ops) == 0 {
		return "", nil
	}
	return ops[0].ProcessID, nil
}
