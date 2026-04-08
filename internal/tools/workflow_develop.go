package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDeployStart reads service metas and creates a deploy session.
// When no metas exist but live services are found, auto-adopts them through the bootstrap engine.
func handleDeployStart(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, input WorkflowInput, cache *ops.StackTypeCache, mounter ops.Mounter, selfHostname string) (*mcp.CallToolResult, any, error) {
	metas, err := workflow.ListServiceMetas(engine.StateDir())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to read service metas: %v", err),
			"Run bootstrap first to create services")), nil, nil
	}

	// Prune stale metas and cache live services for potential auto-adopt.
	var liveServices []platform.ServiceStack
	if client != nil {
		services, listErr := client.ListServices(ctx, projectID)
		if listErr == nil {
			liveServices = services
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
		// Auto-adopt: no metas exist but services may be live on platform.
		if adopted := adoptUnmanagedServices(ctx, engine, client, liveServices, projectID, cache, mounter, selfHostname); adopted {
			metas, err = workflow.ListServiceMetas(engine.StateDir())
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Failed to read metas after auto-adopt: %v", err),
					"")), nil, nil
			}
		}
		if len(metas) == 0 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No bootstrapped services found",
				"Run bootstrap first: action=\"start\" workflow=\"bootstrap\"")), nil, nil
		}
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
// Per spec D2: strategy is informational at start, not a gate.
func buildStrategyStatusNote(metas []*workflow.ServiceMeta) string {
	var unset []string
	strategies := make(map[string]bool)
	for _, m := range metas {
		s := m.EffectiveStrategy()
		if s == "" {
			unset = append(unset, m.Hostname)
		} else {
			strategies[s] = true
		}
	}

	if len(unset) > 0 {
		return fmt.Sprintf(
			"No deploy strategy set for: %s.\n"+
				"Proceed with your code changes. Before deploying, discuss with the user:\n"+
				"- push-dev (SSH self-deploy, quick iterations)\n"+
				"- push-git (git remote + optional CI/CD)\n"+
				"- manual (user controls deployments)\n"+
				"Set via: zerops_workflow action=\"strategy\" strategies={...}",
			strings.Join(unset, ", "))
	}

	// All set — concise summary.
	var names []string
	for s := range strategies {
		names = append(names, s)
	}
	if len(names) == 1 {
		return fmt.Sprintf("Strategy: %s. Change anytime via action=\"strategy\".", names[0])
	}
	return fmt.Sprintf("Strategies: %s. Change anytime via action=\"strategy\".", strings.Join(names, ", "))
}

// adoptUnmanagedServices auto-adopts existing platform services through the bootstrap engine.
// Returns true if adoption occurred, false otherwise. Uses the same code path as manual
// bootstrap adoption: BootstrapStart → BootstrapCompletePlan → BootstrapComplete("provision")
// → fast path. Cleans up on failure to prevent orphaned sessions.
func adoptUnmanagedServices(ctx context.Context, engine *workflow.Engine, client platform.Client, services []platform.ServiceStack, projectID string, cache *ops.StackTypeCache, mounter ops.Mounter, selfHostname string) bool {
	if engine == nil || len(services) == 0 {
		return false
	}
	if engine.HasActiveSession() {
		return false
	}

	// Build adoption candidates from live services.
	var candidates []workflow.AdoptCandidate
	for i := range services {
		if services[i].IsSystem() {
			continue
		}
		if selfHostname != "" && services[i].Name == selfHostname {
			continue
		}
		candidates = append(candidates, workflow.AdoptCandidate{
			Hostname: services[i].Name,
			Type:     services[i].ServiceStackTypeInfo.ServiceStackTypeVersionName,
		})
	}

	targets := workflow.InferServicePairing(candidates)
	if len(targets) == 0 {
		return false
	}

	// Fetch live types for plan validation.
	var liveTypes []platform.ServiceStackType
	if cache != nil {
		liveTypes = cache.Get(ctx, client)
	}

	// Run bootstrap adoption: same engine path as manual bootstrap.
	if _, err := engine.BootstrapStart(projectID, "Auto-adoption of existing services"); err != nil {
		return false
	}

	if _, err := engine.BootstrapCompletePlan(targets, liveTypes, nil); err != nil {
		_ = engine.Reset() // cleanup orphaned bootstrap session
		return false
	}

	if _, err := engine.BootstrapComplete(ctx, "provision", "Auto-adopted: all services exist on platform", nil); err != nil {
		_ = engine.Reset()
		return false
	}

	// Auto-mount runtime services (best-effort, same as manual bootstrap).
	autoMountTargets(ctx, client, projectID, mounter, engine)

	return true
}
