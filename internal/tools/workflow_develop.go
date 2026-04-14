package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDevelopBriefing returns a stateless briefing with no session created.
// The LLM gets knowledge upfront and works freely — no session state machine.
func handleDevelopBriefing(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, _ WorkflowInput, cache *ops.StackTypeCache, mounter ops.Mounter, selfHostname string) (*mcp.CallToolResult, any, error) {
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

	// Clean stale markers from dead processes, then write ours.
	_ = workflow.CleanStaleDevelopMarkers(engine.StateDir())
	if err := workflow.WriteDevelopMarker(engine.StateDir(), projectID, "develop"); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Failed to write develop marker: %v", err),
			"")), nil, nil
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
