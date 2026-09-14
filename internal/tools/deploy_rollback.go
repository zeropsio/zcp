package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// deployStrategyRollbackLabel is the DeployAttempt.Strategy label for the
// GF-8 rollback path (docs/spec-workflows.md §12.6 GF-8): re-activating a
// SPECIFIC recorded appVersion by id. Distinct from
// deployStrategyAppVersionLabel ("appversion", deploy_ssh.go's R2
// newest-only recovery) so attempt history tells the two mechanisms
// apart.
const deployStrategyRollbackLabel = "rollback"

// runAppVersionRollback is the GF-8 rollback dispatch for zerops_deploy
// appVersion=<id> (any value other than "latest", which stays
// runAppVersionRedeploy's R2 recovery in deploy_ssh.go): re-activates
// that SPECIFIC recorded appVersion via ops.ReactivateAppVersion — no
// rebuild, no source resolution, no ledger/tag write (the evidence tag is
// keyed by appVersionId and already maps it to its commit; "what runs" is
// the platform's active appVersion). Shared by both SSH and local deploy
// handlers, mirroring runAppVersionRedeploy's shape.
//
// Returns (result, nil) on a completed (success or classified-failure)
// attempt for the caller to wrap into its own response envelope; (nil,
// blocked) when ReactivateAppVersion itself errored (the caller returns
// blocked verbatim).
func runAppVersionRollback(
	ctx context.Context,
	client platform.Client,
	projectID, stateDir, targetService, appVersionID, mode string,
) (*ops.DeployResult, *mcp.CallToolResult) {
	attempt := workflow.DeployAttempt{
		AttemptedAt:  time.Now().UTC().Format(time.RFC3339),
		Strategy:     deployStrategyRollbackLabel,
		AppVersionID: appVersionID,
	}

	result, err := ops.ReactivateAppVersion(ctx, client, projectID, targetService, appVersionID)
	if err != nil {
		attempt.Error = err.Error()
		_ = workflow.RecordDeployAttempt(stateDir, targetService, attempt)
		return nil, convertError(err, WithRecoveryStatus())
	}
	result.Mode = mode

	switch {
	case result.Status == statusDeployed:
		attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
	case result.TimedOut:
		attempt.Error = deployBuildInFlightMsg
	default:
		attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
		attempt.FailureClass = classifyDeployStatus(result.Status)
	}
	_ = workflow.RecordDeployAttempt(stateDir, targetService, attempt)
	return result, nil
}
