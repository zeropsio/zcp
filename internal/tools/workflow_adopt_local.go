package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleAdoptLocal links one Zerops runtime service as the stage of a
// local-only project. Used to resolve the "multiple runtimes detected"
// ambiguity surfaced by auto-adopt (plan §7.3) — user picks which runtime
// becomes this project's deploy target, we upgrade Mode from local-only
// to local-stage and stamp FirstDeployedAt if the runtime is already
// ACTIVE on the platform.
//
// Refuses in container env (container bootstrap is explicit — adopt-local
// is a local-only concept). Refuses if the meta is already local-stage
// (user would have to explicit-delete their state to relink). Refuses if
// the target hostname isn't a live runtime service (typo, managed
// service, or stale reference).
func handleAdoptLocal(ctx context.Context, client platform.Client, projectID, stateDir string, input WorkflowInput, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	if rt.InContainer {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"action=\"adopt-local\" is for local env — use workflow=\"bootstrap\" in container env",
			"")), nil, nil
	}
	if input.TargetService == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required for action=\"adopt-local\"",
			"Pass targetService=<runtime-hostname> — the Zerops runtime to link as stage for this local project")), nil, nil
	}

	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("read metas: %v", err),
			"")), nil, nil
	}
	if len(metas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No local project meta exists. Auto-adopt runs on server start — restart ZCP, or the API call failed during startup.",
			"")), nil, nil
	}

	// Find the (single) local meta. Local projects have exactly one meta
	// keyed by project name. Container metas are not our concern here.
	var local *workflow.ServiceMeta
	for _, m := range metas {
		if m.Mode == workflow.PlanModeLocalOnly || m.Mode == workflow.PlanModeLocalStage {
			local = m
			break
		}
	}
	if local == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No local-env meta found",
			"action=\"adopt-local\" only applies to local env")), nil, nil
	}
	if local.Mode == workflow.PlanModeLocalStage {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Stage already linked: project %q → %s", local.Hostname, local.StageHostname),
			"To re-link, edit .zcp/state/services/ manually or delete the local meta and restart ZCP")), nil, nil
	}

	// Confirm the target hostname is a live runtime service.
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("list services: %v", err),
			"")), nil, nil
	}
	var target *platform.ServiceStack
	for i, s := range services {
		if s.Name != input.TargetService {
			continue
		}
		if s.IsSystem() {
			continue
		}
		typeName := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if workflow.IsManagedService(typeName) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("%q is a managed service (%s), not a runtime — can't be a deploy target", s.Name, typeName),
				"Pass a runtime service hostname (e.g. an app container, not a db/cache/storage)")), nil, nil
		}
		target = &services[i]
		break
	}
	if target == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("runtime service %q not found in project", input.TargetService),
			"")), nil, nil
	}

	// Upgrade meta: local-only → local-stage, link target.
	local.Mode = workflow.PlanModeLocalStage
	local.StageHostname = target.Name
	// Fresh stage link loses the forced-manual from local-only adoption.
	// Cleared to empty (not the sentinel "unset") so the persisted meta
	// matches a never-configured service exactly — router treats both the
	// same, empty on disk is cleaner.
	local.DeployStrategy = ""
	local.StrategyConfirmed = false
	if target.Status == workflow.StatusActive && local.FirstDeployedAt == "" {
		local.FirstDeployedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := workflow.WriteServiceMeta(stateDir, local); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("write meta: %v", err),
			"")), nil, nil
	}

	return jsonResult(map[string]string{
		"status":   "linked",
		"project":  local.Hostname,
		"stage":    target.Name,
		"mode":     string(workflow.PlanModeLocalStage),
		"strategy": "unset",
		"next":     fmt.Sprintf(`Pick a strategy: zerops_workflow action="strategy" strategies={%q:%q}`, local.Hostname, workflow.StrategyPushDev),
	}), nil, nil
}
