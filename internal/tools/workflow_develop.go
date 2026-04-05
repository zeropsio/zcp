package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDeployStart reads service metas and creates a deploy session.
func handleDeployStart(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	metas, err := workflow.ListServiceMetas(engine.StateDir())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to read service metas: %v", err),
			"Run bootstrap first to create services")), nil, nil
	}

	// Prune stale metas from old bootstrap sessions whose services no longer exist.
	if client != nil {
		services, listErr := client.ListServices(ctx, projectID)
		if listErr == nil {
			live := make(map[string]bool, len(services))
			for _, svc := range services {
				live[svc.Name] = true
			}
			workflow.PruneServiceMetas(engine.StateDir(), live)

			// Re-read after pruning to get the clean list.
			metas, err = workflow.ListServiceMetas(engine.StateDir())
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Failed to read service metas after pruning: %v", err),
					"")), nil, nil
			}
		}
	}

	if len(metas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"No bootstrapped services found",
			"Run bootstrap first: action=\"start\" workflow=\"bootstrap\"")), nil, nil
	}

	// Filter to complete runtime services. Incomplete metas (bootstrap in progress)
	// are skipped — they need bootstrap to finish first.
	var runtimeMetas []*workflow.ServiceMeta
	var skippedIncomplete []string
	for _, m := range metas {
		if !m.IsComplete() {
			skippedIncomplete = append(skippedIncomplete, m.Hostname)
			continue
		}
		if m.Mode != "" || m.StageHostname != "" {
			runtimeMetas = append(runtimeMetas, m)
		}
	}
	if len(runtimeMetas) == 0 {
		msg := "No deployable runtime services found"
		suggestion := "Run bootstrap first: action=\"start\" workflow=\"bootstrap\""
		if len(skippedIncomplete) > 0 {
			msg = fmt.Sprintf("No deployable services — %v still bootstrapping (incomplete)", skippedIncomplete)
			suggestion = "Finish bootstrap for those services first, then start deploy"
		}
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter, msg, suggestion)), nil, nil
	}

	targets, mode := workflow.BuildDeployTargets(runtimeMetas)

	// Enrich targets with runtime types from live API (best-effort).
	if client != nil {
		enrichTargetRuntimeTypes(ctx, client, projectID, targets)
	}

	resp, err := engine.DeployStart(projectID, input.Intent, targets, mode)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Deploy start failed: %v", err),
			"Reset existing session first with action=reset")), nil, nil
	}

	// Append informational strategy status (read fresh from metas, not cached).
	resp.Message += "\n\n" + buildStrategyStatusNote(runtimeMetas)

	return jsonResult(resp), nil, nil
}

func handleDeployComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID, stateDir string, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for deploy complete action",
			"Specify step name (e.g., step=\"prepare\")")), nil, nil
	}
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for deploy complete action",
			"Describe what was accomplished")), nil, nil
	}

	checker := buildDeployStepChecker(input.Step, client, projectID, stateDir)

	resp, err := engine.DeployComplete(ctx, input.Step, input.Attestation, checker)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrDeployNotActive,
			fmt.Sprintf("Deploy complete failed: %v", err),
			"Start deploy first with action=start workflow=deploy")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

func handleDeploySkip(_ context.Context, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for deploy skip action",
			"Specify step name")), nil, nil
	}
	reason := input.Reason
	if reason == "" {
		reason = defaultSkipReason
	}

	resp, err := engine.DeploySkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrDeployNotActive,
			fmt.Sprintf("Deploy skip failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

// enrichTargetRuntimeTypes populates RuntimeType on deploy targets from the live API.
// Best-effort: failures are silently ignored (guidance falls back to generic pointers).
func enrichTargetRuntimeTypes(ctx context.Context, client platform.Client, projectID string, targets []workflow.DeployTarget) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return
	}
	typeMap := make(map[string]string, len(services))
	for _, svc := range services {
		typeMap[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	}
	for i := range targets {
		if rt, ok := typeMap[targets[i].Hostname]; ok {
			targets[i].RuntimeType = rt
		}
	}
}

func handleDeployStatus(_ context.Context, engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	resp, err := engine.DeployStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrDeployNotActive,
			fmt.Sprintf("Deploy status failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

// buildStrategyStatusNote reads strategy from metas and returns a status note.
// Unconfirmed strategies (bootstrap default) get a prompt to discuss with user.
func buildStrategyStatusNote(metas []*workflow.ServiceMeta) string {
	var unconfirmed []string
	strategies := make(map[string]bool)
	allConfirmed := true
	for _, m := range metas {
		strategies[m.DeployStrategy] = true
		if !m.StrategyConfirmed {
			unconfirmed = append(unconfirmed, m.Hostname)
			allConfirmed = false
		}
	}

	if !allConfirmed {
		return fmt.Sprintf(
			"REQUIRED: Before deploying, confirm deploy strategy with the user.\n"+
				"Services %s use push-dev (default from bootstrap).\n"+
				"Ask the user: keep push-dev, or switch to push-git (git remote + optional CI/CD) or manual?\n"+
				"Set via: zerops_workflow action=\"strategy\" strategies={...}\n"+
				"Strategy can be changed anytime later.",
			unconfirmed)
	}

	// All confirmed — concise summary.
	var names []string
	for s := range strategies {
		names = append(names, s)
	}
	summary := fmt.Sprintf("Strategy: %s.", names[0])
	if len(names) > 1 {
		summary = fmt.Sprintf("Strategies: %s.", strings.Join(names, ", "))
	}
	return summary + " Change anytime via action=\"strategy\"."
}
