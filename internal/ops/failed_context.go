package ops

import (
	"context"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// FailedDeployContext is the structured failure summary attached to the most
// recent failed appVersion for a given service. Returned by
// LatestFailedAppVersionContext; consumed by lifecycle gates
// (workflow_checks status rejection, deploy pre-flight, dev_server pre-spawn,
// import override gate) to surface diagnostic context BEFORE proposing
// destructive recovery.
//
// Lives in ops because the classifier path it reuses
// (FailurePhaseFromStatus + ClassifyDeployFailure) is here. The discriminator
// "service has any failed history" is the empirical priority for diagnose-
// before-destruct gates: healthy services don't need diagnosis, only services
// with failure history do (plan v4 §3.1).
type FailedDeployContext struct {
	// FailedAt is the parsed timestamp of the most recent failed appVersion.
	FailedAt time.Time
	// FailureClass is the classifier's coarse category (build/start/etc.).
	FailureClass topology.FailureClass
	// FailureCause is the classifier's one-sentence diagnosis.
	FailureCause string
}

// failedContextLimit is the appVersion search window for failed-history
// detection. Per plan v4 §1.3 — single SearchAppVersions call per pre-flight
// gate; 10 entries cover typical service churn comfortably.
const failedContextLimit = 10

// appVersionSourceNone is the marker for `startWithoutCode: true` bootstrap
// stamps that carry no real build (Source="NONE", Build=nil). Skipped by
// timeline construction (events.go) and failed-history scans alike — they
// are never failures.
const appVersionSourceNone = "NONE"

// appVersionStatusWaitingToBuild is the appVersion status of a build that was
// queued but never produced a recognized failure phase (FailurePhaseFromStatus
// returns "" for it). A stuck WAITING_TO_BUILD whose underlying build PROCESS
// FAILED is a build failure the appVersion status alone can't see — the P2
// fallback consults SearchProcesses to tell genuinely-queued from stuck-failed.
const appVersionStatusWaitingToBuild = "WAITING_TO_BUILD"

// actionStackBuild is the Zerops API action name for the build pipeline (per
// events.go::actionNameMap). The WAITING_TO_BUILD fallback matches a FAILED
// process with this action bound to the target's serviceStackId.
const actionStackBuild = "stack.build"

// LatestFailedAppVersionContext returns the most-recent failed appVersion's
// classification + a suggested-read tool hint for the named service, or nil
// when no failed history exists.
//
// Reuses the existing classification path (FailurePhaseFromStatus +
// ClassifyDeployFailure) so async webhook builds, sync deploy responses, and
// pre-flight gates all emit the same diagnostic vocabulary. fetcher feeds
// FetchBuildLogs to enrich the classifier when the failure has recognizable
// patterns; pass nil to skip log enrichment (callers that just need the
// failure-class verdict — e.g. a workflow_checks rejection — don't need
// LikelyCause text refinement, and the phase baseline is sufficient).
//
// Returns (nil, nil) when:
//   - hostname is not in the project's service list
//   - no failed appVersion exists for the resolved serviceStackId within the
//     latest failedContextLimit entries (filters out startWithoutCode stamps
//     mirroring ops/events.go semantics)
//
// Returns (nil, err) only when the platform API call itself fails.
func LatestFailedAppVersionContext(
	ctx context.Context,
	client platform.Client,
	fetcher platform.LogFetcher,
	projectID, hostname string,
) (*FailedDeployContext, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var serviceStackID string
	for _, s := range services {
		if s.Name == hostname {
			serviceStackID = s.ID
			break
		}
	}
	if serviceStackID == "" {
		return nil, nil //nolint:nilnil // not-found sentinel: hostname doesn't resolve to a service in this project
	}

	appVersions, err := client.SearchAppVersions(ctx, projectID, failedContextLimit)
	if err != nil {
		return nil, err
	}

	// API returns sorted desc by created — first match wins.
	sawWaitingToBuild := false
	for i := range appVersions {
		av := appVersions[i]
		if av.ServiceStackID != serviceStackID {
			continue
		}
		// Mirror ops/events.go: skip startWithoutCode appVersions
		// (Source="NONE", no build info) — bootstrap stamps, not real builds.
		if av.Source == appVersionSourceNone && av.Build == nil {
			continue
		}
		phase := FailurePhaseFromStatus(av.Status)
		if phase == "" {
			// A WAITING_TO_BUILD appVersion has no failure phase — but its build
			// PROCESS may have FAILED (the recover-failed stuck case). Remember
			// we saw one so the post-loop fallback can consult SearchProcesses.
			if av.Status == appVersionStatusWaitingToBuild {
				sawWaitingToBuild = true
			}
			continue
		}

		var buildLogs []string
		if fetcher != nil {
			buildLogs = FetchBuildLogs(ctx, client, fetcher, projectID, &av, 200)
		}
		cls := ClassifyDeployFailure(FailureInput{
			Phase:     phase,
			Status:    av.Status,
			BuildLogs: buildLogs,
		})
		if cls == nil {
			continue
		}

		failedAt, _ := parseTimestamp(av.Created)
		return &FailedDeployContext{
			FailedAt:     failedAt,
			FailureClass: cls.Category,
			FailureCause: cls.LikelyCause,
		}, nil
	}

	// P2 fallback: no recognized failure phase, but the service has an
	// appVersion stuck in WAITING_TO_BUILD. The appVersion status can't tell
	// "queued, still building" from "build process already failed" — only the
	// process list can. Consult SearchProcesses for a FAILED build process bound
	// to this serviceStackId: present → classify as a build failure (the gate's
	// per-target class is now non-nil); absent → genuinely queued, return nil
	// (no false failure verdict). This is the queued-vs-stuck guard.
	if sawWaitingToBuild {
		processes, perr := client.SearchProcesses(ctx, projectID, failedContextLimit)
		if perr != nil {
			return nil, perr
		}
		if failedBuildProcessForStack(processes, serviceStackID) {
			return &FailedDeployContext{
				FailureClass: topology.FailureClassBuild,
				FailureCause: "Build pipeline failed (appVersion stuck in WAITING_TO_BUILD with a FAILED build process).",
			}, nil
		}
	}

	return nil, nil //nolint:nilnil // not-found sentinel: no failed appVersion in scope window
}

// failedBuildProcessForStack reports whether the process list contains a FAILED
// build process (action stack.build) bound to the given serviceStackId. The
// queued-vs-stuck discriminator for the WAITING_TO_BUILD fallback — only a
// FAILED build process turns a stuck appVersion into a build-failure verdict.
func failedBuildProcessForStack(processes []platform.ProcessEvent, serviceStackID string) bool {
	for i := range processes {
		p := processes[i]
		if p.Status != platform.ProcessStatusFailed || p.ActionName != actionStackBuild {
			continue
		}
		for _, ref := range p.ServiceStacks {
			if ref.ID == serviceStackID {
				return true
			}
		}
	}
	return false
}

// HasPriorDeployAttempt returns true when the named service has any
// non-startWithoutCode appVersion within the scope window, regardless of
// status. The discriminator answers "has this service ever tried to deploy?"
// — broader than LatestFailedAppVersionContext (which only catches recognized
// failure phases). Used by NonRunningRecovery + the import-override gate to
// treat a service in READY_TO_DEPLOY with any prior deploy history (including
// stalled states like WAITING_TO_BUILD with null pipelineStart — Karel's
// 2026-05-16 reproducer) as holding code/config worth preserving: the recovery
// reads-first (zerops_events) and the override gate requires confirmDestructive,
// versus pointing at zerops_logs for the rare never-deployed case where logs
// carry no useful diagnostic data.
//
// Phase 2.2 of plans/eval-review-20260518-subset/fix-plan.md broadened the
// discriminator: previously the develop-adopt path's recovery hint relied on
// LatestFailedAppVersionContext != nil, which silently filtered out queued
// states because FailurePhaseFromStatus returns "" for them. The launch-
// production path got a parallel fix via 33fb9358 (launchFirstDeployFailedResponse)
// but the symmetric develop-adopt path missed the same coverage.
//
// Returns (false, err) only when the platform API call itself fails.
func HasPriorDeployAttempt(
	ctx context.Context,
	client platform.Client,
	projectID, hostname string,
) (bool, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return false, err
	}
	var serviceStackID string
	for _, s := range services {
		if s.Name == hostname {
			serviceStackID = s.ID
			break
		}
	}
	if serviceStackID == "" {
		return false, nil
	}

	appVersions, err := client.SearchAppVersions(ctx, projectID, failedContextLimit)
	if err != nil {
		return false, err
	}
	for i := range appVersions {
		av := appVersions[i]
		if av.ServiceStackID != serviceStackID {
			continue
		}
		// Mirror LatestFailedAppVersionContext + events.go: skip
		// startWithoutCode appVersions (Source="NONE", no build info) —
		// bootstrap stamps, not real deploy attempts.
		if av.Source == appVersionSourceNone && av.Build == nil {
			continue
		}
		return true, nil
	}
	return false, nil
}
