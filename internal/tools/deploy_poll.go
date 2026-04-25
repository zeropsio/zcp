package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const buildContainerSource = "build_container"

// pollDeployBuild polls build status after deploy trigger and updates result in-place.
// sshDeployer can be nil (local mode) — WaitSSHReady is skipped when nil.
func pollDeployBuild(
	ctx context.Context,
	client platform.Client,
	projectID string,
	result *ops.DeployResult,
	onProgress ops.ProgressCallback,
	logFetcher platform.LogFetcher,
	sshDeployer ops.SSHDeployer,
) {
	if result.TargetServiceID == "" {
		return
	}

	event, err := ops.PollBuild(ctx, client, projectID, result.TargetServiceID, onProgress)
	if err != nil {
		// Timeout or context cancellation — keep original BUILD_TRIGGERED status.
		result.TimedOut = true
		return
	}

	result.BuildStatus = event.Status
	result.BuildDuration = calcBuildDuration(event)

	if event.Status == statusActive {
		result.Status = statusDeployed
		result.MonitorHint = ""
		// Post-deploy message is runtime-class-agnostic and strategy-agnostic
		// (invariant DS-01, plans/dev-server-canonical-primitive.md).
		// Dev-server lifecycle guidance is owned by atoms: they prescribe
		// `zerops_dev_server` in container env and the harness background
		// task primitive in local env. zerops_verify covers runtime-state
		// assertions honestly (service_running, error_logs, http_root).
		// The message here reports only what the platform told us.
		result.Message = fmt.Sprintf("Successfully deployed to %s. Run zerops_verify for runtime state.", result.TargetService)
		if result.SourceService == result.TargetService {
			// Strategy-agnostic fact: push-dev replaces the container, which
			// drops any prior SSH sessions. Agents holding open sessions
			// from before the deploy need to reconnect.
			result.Message += " New container replaced old — prior SSH sessions are gone."
		}
		result.NextActions = deploySuccessNextActions(result)
		// Fetch build warnings/errors even on success (best-effort).
		// Surfaces issues like silent build failures, missing deployFiles output.
		if logFetcher != nil {
			// Limit=100: client-side tag filter is already scoping to this
			// build; 100 is comfortable headroom for chatty builds.
			buildWarnings := ops.FetchBuildWarnings(ctx, client, logFetcher, projectID, event, 100)
			if len(buildWarnings) > 0 {
				result.BuildLogs = buildWarnings
				result.BuildLogsSource = buildContainerSource
			}
		}
		if sshDeployer != nil {
			if err := ops.WaitSSHReady(ctx, sshDeployer, result.TargetService); err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("SSH not ready on %s after 30s — deployed but SSH may need more time", result.TargetService))
			} else {
				result.SSHReady = true
			}
		}
	} else {
		// Any non-ACTIVE status is a failure — preserve actual API status.
		result.Status = event.Status
		result.FailedPhase = failedPhaseForStatus(event.Status)
		if logFetcher != nil {
			// Fetch logs from the right container based on failure phase.
			// BUILD_FAILED + PREPARING_RUNTIME_FAILED: build container has the stderr.
			// DEPLOY_FAILED: runtime container has the initCommand stderr.
			switch event.Status {
			case statusDeployFailed:
				// Anchor the runtime log fetch to the current container's
				// creation time so stale crashes from a previous deploy's
				// container do not bleed in. Phase 5 will swap this to
				// event.Build.ContainerCreationStart; for now PipelineFinish
				// approximates (container is created immediately after build).
				creation := containerCreationAnchor(event)
				result.RuntimeLogs = ops.FetchRuntimeLogs(ctx, client, logFetcher, projectID, result.TargetServiceID, creation, 50)
				if len(result.RuntimeLogs) > 0 {
					result.RuntimeLogsSource = "runtime_container"
				}
			default:
				result.BuildLogs = ops.FetchBuildLogs(ctx, client, logFetcher, projectID, event, 50)
				if len(result.BuildLogs) > 0 {
					result.BuildLogsSource = buildContainerSource
				}
			}
		}
		hasLogs := len(result.BuildLogs) > 0 || len(result.RuntimeLogs) > 0
		result.Suggestion = deploySuggestionForStatus(event.Status, hasLogs)
		result.NextActions = deployNextActionForStatus(event.Status)
	}
}

// failedPhaseForStatus maps app version status to a short phase identifier.
func failedPhaseForStatus(status string) string {
	switch status {
	case statusBuildFailed:
		return "build"
	case statusPreparingRuntimeFailed:
		return "prepare"
	case statusDeployFailed:
		return "init"
	}
	return ""
}

// containerCreationAnchor returns the authoritative Since anchor for a
// FetchRuntimeLogs call. Order of preference (most → least precise):
//  1. Build.ContainerCreationStart — exact "new container begins here".
//  2. Build.PipelineFinish — container spins up immediately after build.
//  3. Build.PipelineFailed — if build failed, use that as upper bound.
//  4. Build.PipelineStart — earliest sensible anchor.
//
// Zero time means no anchor available; the caller receives unanchored
// (best-effort) runtime logs rather than an error.
func containerCreationAnchor(event *platform.AppVersionEvent) time.Time {
	if event == nil || event.Build == nil {
		return time.Time{}
	}
	for _, raw := range []*string{
		event.Build.ContainerCreationStart,
		event.Build.PipelineFinish,
		event.Build.PipelineFailed,
		event.Build.PipelineStart,
	} {
		if raw == nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339, *raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

// calcBuildDuration computes the build pipeline duration from event build info.
func calcBuildDuration(event *platform.AppVersionEvent) string {
	if event.Build == nil || event.Build.PipelineStart == nil {
		return ""
	}
	start, err := time.Parse(time.RFC3339, *event.Build.PipelineStart)
	if err != nil {
		return ""
	}
	var endStr string
	switch {
	case event.Build.PipelineFinish != nil:
		endStr = *event.Build.PipelineFinish
	case event.Build.PipelineFailed != nil:
		endStr = *event.Build.PipelineFailed
	default:
		return ""
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return ""
	}
	return end.Sub(start).Truncate(time.Second).String()
}
