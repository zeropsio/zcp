package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDevelopBriefing returns the develop briefing and creates/resumes a
// per-PID WorkSession that records deploy/verify lifecycle for the task.
//
// The WorkSession survives context compaction via the system-prompt
// "Lifecycle Status" block, so the LLM never forgets what was deployed and
// what still needs verification — even across summarization boundaries.
//
// Guidance is synthesized via the Layer 2 atom pipeline (ComputeEnvelope →
// Synthesize → BuildPlan → RenderStatus): runtime, strategy, mode and
// environment axes of the envelope drive which atoms match.
func handleDevelopBriefing(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, input WorkflowInput, cache *ops.StackTypeCache, mounter ops.Mounter, selfHostname string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
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

	// Filter to complete runtime services. Required for both the strategy gate
	// and the WorkSession scope — managed services are never deploy targets.
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

	// Strategy gate (spec-work-session.md §6.1): if ANY runtime service has no
	// confirmed strategy, render the briefing WITHOUT creating a work session.
	// The atom pipeline picks up the `strategies: [unset]` atom because
	// ComputeEnvelope sets snapshot.Strategy=StrategyUnset for empty metas.
	strategyUnset := false
	for _, m := range runtimeMetas {
		if m.EffectiveStrategy() == "" {
			strategyUnset = true
			break
		}
	}

	if !strategyUnset {
		// Create or resume per-PID WorkSession.
		//
		// If an open session already exists for this PID:
		//   - matching / empty intent → reuse (fresh briefing, same session)
		//   - different intent        → refuse and ask the LLM to close first
		// Closed sessions are overwritten: the prior task is done, a new intent
		// means a new task.
		existing, _ := workflow.CurrentWorkSession(engine.StateDir())
		if existing != nil && existing.ClosedAt == "" {
			if input.Intent != "" && existing.Intent != "" && existing.Intent != input.Intent {
				return convertError(platform.NewPlatformError(
					platform.ErrWorkflowActive,
					fmt.Sprintf("Active work session with different intent: %q", existing.Intent),
					"Close the current task first: zerops_workflow action=\"close\" workflow=\"develop\"")), nil, nil
			}
		} else {
			scope := workSessionScopeFromMetas(runtimeMetas)
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
	}

	// Render via the atom pipeline: envelope → atom filter → typed plan → markdown.
	envelope, err := workflow.ComputeEnvelope(ctx, client, engine.StateDir(), projectID, rt, time.Now())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Compute envelope: %v", err),
			"")), nil, nil
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Load knowledge atoms: %v", err),
			"")), nil, nil
	}
	guidance, err := workflow.Synthesize(envelope, corpus)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Synthesize guidance: %v", err),
			"")), nil, nil
	}
	plan := workflow.BuildPlan(envelope)
	return textResult(workflow.RenderStatus(workflow.Response{
		Envelope: envelope,
		Guidance: guidance,
		Plan:     &plan,
	})), nil, nil
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

	// Fetch live types up front so classification (F4) and plan validation
	// both use the same authoritative catalog. InferServicePairing relies on
	// liveManaged to recognize new Zerops categories without a static bump.
	var liveTypes []platform.ServiceStackType
	if cache != nil {
		liveTypes = cache.Get(ctx, client)
	}
	liveManaged := knowledge.ManagedBaseNames(liveTypes)

	targets := workflow.InferServicePairing(candidates, liveManaged)
	if len(targets) == 0 {
		return false
	}

	// Run bootstrap adoption: same engine path as manual bootstrap, but
	// pre-commits route=adopt because we already derived targets from the
	// existing services. The LLM didn't make the choice — it's an internal
	// shortcut when develop notices an all-existing plan.
	if _, err := engine.BootstrapStartWithRoute(projectID, "Auto-adoption of existing services", workflow.BootstrapRouteAdopt, ""); err != nil {
		return false
	}

	// F6: pass live services for EXISTS validation — prevents a race where a
	// hostname disappears between discover and develop.
	if _, err := engine.BootstrapCompletePlan(targets, liveTypes, services); err != nil {
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

// workSessionScopeFromMetas returns the ordered hostnames a work session tracks
// for auto-close — dev/simple targets first, stage targets second. Stage is
// included because in standard mode deploy+verify must cover both.
func workSessionScopeFromMetas(metas []*workflow.ServiceMeta) []string {
	scope := make([]string, 0, len(metas)*2)
	for _, m := range metas {
		scope = append(scope, m.Hostname)
	}
	for _, m := range metas {
		if m.StageHostname != "" {
			scope = append(scope, m.StageHostname)
		}
	}
	return scope
}
