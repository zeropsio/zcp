package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
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
			""), WithRecoveryStatus()), nil, nil
	}
	if input.TargetService == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required for action=\"adopt-local\"",
			"Pass targetService=<runtime-hostname> — the Zerops runtime to link as stage for this local project"), WithRecoveryStatus()), nil, nil
	}

	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("read metas: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	if len(metas) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No local project meta exists. Auto-adopt runs on server start — restart ZCP, or the API call failed during startup.",
			""), WithRecoveryStatus()), nil, nil
	}

	// Find the (single) local meta. Local projects have exactly one meta
	// keyed by project name. Container metas are not our concern here.
	var local *workflow.ServiceMeta
	for _, m := range metas {
		if m.Mode == topology.PlanModeLocalOnly || m.Mode == topology.PlanModeLocalStage {
			local = m
			break
		}
	}
	if local == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No local-env meta found",
			"action=\"adopt-local\" only applies to local env"), WithRecoveryStatus()), nil, nil
	}
	if local.Mode == topology.PlanModeLocalStage {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Stage already linked: project %q → %s", local.Hostname, local.StageHostname),
			"To re-link, edit .zcp/state/services/ manually or delete the local meta and restart ZCP"), WithRecoveryStatus()), nil, nil
	}

	// Confirm the target hostname is a live runtime service.
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("list services: %v", err),
			""), WithRecoveryStatus()), nil, nil
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
		if topology.IsManagedService(typeName) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("%q is a managed service (%s), not a runtime — can't be a deploy target", s.Name, typeName),
				"Pass a runtime service hostname (e.g. an app container, not a db/cache/storage)"), WithRecoveryStatus()), nil, nil
		}
		target = &services[i]
		break
	}
	if target == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("runtime service %q not found in project", input.TargetService),
			""), WithRecoveryStatus()), nil, nil
	}

	// Upgrade meta: local-only → local-stage, link target.
	local.Mode = topology.PlanModeLocalStage
	local.StageHostname = target.Name
	// Fresh stage link clears the forced-manual close-mode from local-only
	// adoption — once a Zerops runtime is linked, auto / git-push become
	// valid choices, so reset to unset and let the develop-strategy-review
	// atom prompt the agent on the next status round-trip.
	local.CloseDeployMode = topology.CloseModeUnset
	local.CloseDeployModeConfirmed = false
	if target.Status == workflow.StatusActive && local.FirstDeployedAt == "" {
		local.FirstDeployedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := workflow.WriteServiceMeta(stateDir, local); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("write meta: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(adoptLocalResponse{
		Status:    "linked",
		Project:   local.Hostname,
		Stage:     target.Name,
		Mode:      topology.PlanModeLocalStage,
		CloseMode: topology.CloseModeUnset,
	}), nil, nil
}

// G11: response carries no `next` hint. CloseDeployMode is unset on
// purpose; the develop-strategy-review atom (deployStates=deployed,
// closeDeployModes=[unset]) fires on the next `zerops_workflow
// action="status"` and surfaces the per-service close-mode prompt with
// full envelope context.
type adoptLocalResponse struct {
	Status    string                   `json:"status"`
	Project   string                   `json:"project"`
	Stage     string                   `json:"stage"`
	Mode      topology.Mode            `json:"mode"`
	CloseMode topology.CloseDeployMode `json:"closeMode"`
}
