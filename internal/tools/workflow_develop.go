package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDevelopBriefing returns the develop briefing and creates/resumes a
// per-PID WorkSession that records deploy/verify lifecycle for the task.
//
// The WorkSession survives context compaction via the system-prompt
// "Lifecycle Status" block, so the LLM never forgets what was deployed and
// what still needs verification — even across summarization boundaries.
func handleDevelopBriefing(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, input WorkflowInput, cache *ops.StackTypeCache, mounter ops.Mounter, selfHostname string) (*mcp.CallToolResult, any, error) {
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
				platform.ErrPrerequisiteMissing,
				"No bootstrapped services found",
				"Run bootstrap first: action=\"start\" workflow=\"bootstrap\"")), nil, nil
		}
	}

	// Filter to complete runtime services.
	var runtimeMetas []*workflow.ServiceMeta
	for _, m := range metas {
		if !m.IsComplete() {
			continue
		}
		if m.Mode != "" || m.StageHostname != "" {
			runtimeMetas = append(runtimeMetas, m)
		}
	}
	if len(runtimeMetas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No deployable runtime services found",
			"Run bootstrap first: action=\"start\" workflow=\"bootstrap\"")), nil, nil
	}

	// Build briefing targets from metas.
	targets, mode := workflow.BuildBriefingTargets(runtimeMetas)

	// Enrich targets with runtime types and HTTP support from live service data.
	typeMap := make(map[string]string, len(liveServices))
	httpMap := make(map[string]bool, len(liveServices))
	for _, svc := range liveServices {
		typeMap[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		for _, p := range svc.Ports {
			if p.HTTPSupport {
				httpMap[svc.Name] = true
				break
			}
		}
	}
	workflow.EnrichBriefingTargets(targets, typeMap, httpMap)

	// Determine dominant strategy.
	strategy := ""
	for _, t := range targets {
		if t.Role != workflow.DeployRoleStage && t.Strategy != "" {
			strategy = t.Strategy
			break
		}
	}

	// Create or resume per-PID WorkSession.
	//
	// If an open session already exists for this PID:
	//   - matching / empty intent → reuse (fresh briefing, same session)
	//   - different intent        → refuse and ask the LLM to close first
	// Closed sessions are overwritten: the prior task is done, a new intent
	// means a new task.
	scope := workSessionScope(targets)
	existing, _ := workflow.CurrentWorkSession(engine.StateDir())
	if existing != nil && existing.ClosedAt == "" {
		if input.Intent != "" && existing.Intent != "" && existing.Intent != input.Intent {
			return convertError(platform.NewPlatformError(
				platform.ErrWorkflowActive,
				fmt.Sprintf("Active work session with different intent: %q", existing.Intent),
				"Close the current task first: zerops_workflow action=\"close\" workflow=\"develop\"")), nil, nil
		}
	} else {
		ws := workflow.NewWorkSession(projectID, string(engine.Environment()), input.Intent, scope)
		if err := workflow.SaveWorkSession(engine.StateDir(), ws); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Failed to save work session: %v", err),
				"")), nil, nil
		}
		_ = workflow.RegisterSession(engine.StateDir(), workflow.SessionEntry{
			SessionID: workflow.WorkSessionID(os.Getpid()),
			PID:       os.Getpid(),
			Workflow:  workflow.WorkflowWork,
			ProjectID: projectID,
			Intent:    input.Intent,
		})
	}

	briefingText := workflow.BuildDevelopBriefing(targets, strategy, mode, engine.Environment(), engine.StateDir())

	return jsonResult(workflow.DevelopBriefing{
		Targets:  targets,
		Mode:     mode,
		Strategy: strategy,
		Briefing: briefingText,
	}), nil, nil
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

// workSessionScope returns the ordered list of hostnames a work session tracks
// for auto-close — dev/simple targets first, stage targets second. Stage is
// included because in standard mode deploy+verify must cover both.
func workSessionScope(targets []workflow.BriefingTarget) []string {
	scope := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.Role == workflow.DeployRoleStage {
			continue
		}
		scope = append(scope, t.Hostname)
	}
	for _, t := range targets {
		if t.Role == workflow.DeployRoleStage {
			scope = append(scope, t.Hostname)
		}
	}
	return scope
}
