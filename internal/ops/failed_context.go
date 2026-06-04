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
	// SuggestedReadTool is the MCP tool the agent should call next to read
	// the underlying failure output. Always populated when the helper
	// returns non-nil; "zerops_logs" for build/prepare/init phase failures.
	SuggestedReadTool string
	// SuggestedArgs is the argument set for SuggestedReadTool, keyed for
	// direct passthrough into the MCP tool input (`serviceHostname`,
	// `facility`, `severity`, `since`).
	SuggestedArgs map[string]string
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

// logFacilityApplication is the log facility for application-tier output
// (build container, runtime container). Used in Recovery hint args and
// FetchBuildLogs scoping. The literal `"application"` is also pinned in
// build_logs.go by an AST contract test — kept as a string there so the
// contract test stays valid.
const logFacilityApplication = "application"

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
			FailedAt:          failedAt,
			FailureClass:      cls.Category,
			FailureCause:      cls.LikelyCause,
			SuggestedReadTool: "zerops_logs",
			SuggestedArgs:     suggestedReadArgs(phase, hostname),
		}, nil
	}

	return nil, nil //nolint:nilnil // not-found sentinel: no failed appVersion in scope window
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

// suggestedReadArgs builds the MCP-tool argument map for the diagnostic
// deep-dive call the agent should issue next. Phase-specific because the
// log facility / severity that surfaces the failing output differs:
//   - build / prepare ran in the build container → facility=application
//   - init crashed the runtime container → severity=ERROR (DEPLOY_FAILED
//     hint in events.go appVersionHintMap mirrors this)
//
// Transport / preflight phases never reach this helper — failed-history
// scans only walk completed builds, and those phases never produce an
// AppVersion event.
func suggestedReadArgs(phase DeployFailurePhase, hostname string) map[string]string {
	args := map[string]string{
		"serviceHostname": hostname,
		"since":           "15m",
	}
	switch phase {
	case PhaseBuild, PhasePrepare:
		args["facility"] = logFacilityApplication
	case PhaseInit:
		args["severity"] = "ERROR"
	case PhaseTransport, PhasePreflight:
		// Unreachable in this caller path — these phases never produce
		// an AppVersion event the failed-history scan walks.
	}
	return args
}
