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
		isSelfDeploy := result.SourceService == result.TargetService
		if isSelfDeploy && ops.NeedsManualStart(result.TargetServiceType) {
			result.Message = fmt.Sprintf("Successfully deployed to %s. Container restarted — dev server NOT running.", result.TargetService)
		} else {
			result.Message = fmt.Sprintf("Successfully deployed to %s", result.TargetService)
		}
		result.NextActions = deploySuccessNextActions(result)
		// Fetch build warnings/errors even on success (best-effort).
		// Surfaces issues like silent build failures, missing deployFiles output.
		if logFetcher != nil {
			buildWarnings := ops.FetchBuildWarnings(ctx, client, logFetcher, projectID, event, 20)
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
				result.RuntimeLogs = ops.FetchRuntimeLogs(ctx, client, logFetcher, projectID, result.TargetServiceID, 50)
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
