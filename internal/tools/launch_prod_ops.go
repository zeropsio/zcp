package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// F7 — the bring-up management window. While a launched (or failed)
// production project is being brought up, the operator can manage it
// THROUGH ZCP over the public REST API: list services, read logs, read
// env keys, restart/stop/start a service, delete a botched service.
// Everything runs with a PER-CALL launchKey (P-LP-1: never persisted —
// re-supplied on every call, client constructed + Closed per call);
// the non-secret target identity (TargetProjectID, service handles)
// comes from the persisted launch state. SSH/VPN surfaces stay out of
// scope — the public API covers every operation here.
//
// Whole-project deletion deliberately stays on action="reset" (the
// diagnose-before-destruct gate with wouldDestroy + confirmDestructive
// already owns it); prod-ops points there instead of duplicating the
// ack flow.

// prod-ops operation identifiers (single owner — also used in response
// bodies + the dispatch switch so goconst stays satisfied).
const (
	prodOpStatus        = "status"
	prodOpLogs          = "logs"
	prodOpEnvKeys       = "env-keys"
	prodOpRestart       = "restart"
	prodOpStop          = "stop"
	prodOpStart         = "start"
	prodOpScale         = "scale"
	prodOpDeleteService = "delete-service"
)

// prodOpsOperations is the closed set of prod-ops operations.
var prodOpsOperations = map[string]bool{
	prodOpStatus:        true,
	prodOpLogs:          true,
	prodOpEnvKeys:       true,
	prodOpRestart:       true,
	prodOpStop:          true,
	prodOpStart:         true,
	prodOpScale:         true,
	prodOpDeleteService: true,
}

// handleLaunchProdOps dispatches action="prod-ops". Required inputs:
// productionProjectName (locates the launch state), launchKey (per-call
// credential), prodOperation; service-targeting ops also need
// targetService (the PROD hostname).
func handleLaunchProdOps(
	ctx context.Context,
	projectID string,
	logFetcher platform.LogFetcher,
	input WorkflowInput,
	stateDir string,
	apiHost string,
) (*mcp.CallToolResult, any, error) {
	if input.ProductionProjectName == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"prod-ops requires productionProjectName (locates the launch state for the target production project)",
			"Re-call with productionProjectName=\"<name>\" prodOperation=\"status|logs|env-keys|restart|stop|start|delete-service\" launchKey=<key>.",
		), WithRecoveryStatus()), nil, nil
	}
	op := strings.TrimSpace(input.ProdOperation)
	if !prodOpsOperations[op] {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("prodOperation %q is not a prod-ops operation", input.ProdOperation),
			"Valid operations: status, logs, env-keys, restart, stop, start, delete-service. Whole-project deletion goes through action=\"reset\" (diagnose-before-destruct).",
		), WithRecoveryStatus()), nil, nil
	}

	launchID := generateLaunchID(projectID, input.ProductionProjectName)
	state, err := readLaunchState(stateDir, launchID)
	if err != nil || state == nil || state.TargetProjectID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("no launch state with a created production project for %q — prod-ops manages a project the launch workflow created", input.ProductionProjectName),
			"Run the launch first (zerops_workflow action=\"start\" workflow=\"launch-production\"), or check productionProjectName spelling. The bring-up window exists only between project create and key revoke.",
		), WithRecoveryStatus()), nil, nil
	}

	if input.LaunchKey == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"prod-ops requires launchKey on EVERY call — the key is never persisted (it lives only inside this request)",
			"Re-call with the launch-window key as launchKey=<value>. If you already revoked it, the bring-up window is closed — manage the production project in the Zerops dashboard (or supply a project-scoped token via the existing-project path).",
		), WithRecoveryStatus()), nil, nil
	}

	admin, adminErr := projectAdminClientFactory(input.LaunchKey, apiHost)
	if adminErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("launch key rejected: %v", adminErr),
			"The key must be the launch-window token (Custom access per project + Allow creating projects). Ask the user for the current value — never invent one.",
		), WithRecoveryStatus()), nil, nil
	}
	defer admin.Close()

	switch op {
	case prodOpStatus:
		return prodOpsStatus(ctx, admin, state), nil, nil
	case prodOpLogs:
		return prodOpsLogs(ctx, admin, logFetcher, state, input), nil, nil
	case prodOpEnvKeys:
		return prodOpsEnvKeys(ctx, admin, state, input), nil, nil
	case prodOpRestart, prodOpStop, prodOpStart:
		return prodOpsLifecycle(ctx, admin, state, input, op), nil, nil
	case prodOpScale:
		return prodOpsScale(ctx, admin, state, input), nil, nil
	case prodOpDeleteService:
		return prodOpsDeleteService(ctx, admin, state, input), nil, nil
	}
	// Unreachable — prodOpsOperations gate above.
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter, "unhandled prod-ops operation", "")), nil, nil
}

// prodOpsResolveService maps the requested PROD hostname to its live
// service in the target project.
func prodOpsResolveService(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, hostname string) (*platform.ServiceStack, *mcp.CallToolResult) {
	if hostname == "" {
		return nil, convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"this prod-ops operation requires targetService (the PRODUCTION service hostname)",
			"Run prodOperation=\"status\" first to list the prod services, then re-call with targetService set.",
		), WithRecoveryStatus())
	}
	services, err := admin.ListServices(ctx, state.TargetProjectID)
	if err != nil {
		return nil, convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	for i := range services {
		if services[i].Name == hostname {
			return &services[i], nil
		}
	}
	names := make([]string, 0, len(services))
	for _, s := range services {
		names = append(names, s.Name)
	}
	return nil, convertError(platform.NewPlatformError(
		platform.ErrServiceNotFound,
		fmt.Sprintf("service %q not found in production project %s — available: %s", hostname, state.TargetProjectID, strings.Join(names, ", ")),
		"Use one of the listed production hostnames.",
	), WithRecoveryStatus())
}

// prodOpsTranslateErr converts the A.10 projectNotFound shape into the
// honest diagnosis: the project exists; the launching user's ADMIN role
// grant is missing (GrantSelfRole failed at launch), so reads 404.
func prodOpsTranslateErr(err error, state *launchState) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("the platform answered not-found for project %s — this usually means the ADMIN role grant from launch time is missing (the project exists; the calling token has no role on it)", state.TargetProjectID),
			"Open the project in the Zerops dashboard to confirm it exists, then re-run the launch resume (the role grant retries there), or manage via dashboard.",
		)
	}
	return err
}

func prodOpsStatus(ctx context.Context, admin platform.ProjectAdminClient, state *launchState) *mcp.CallToolResult {
	services, err := admin.ListServices(ctx, state.TargetProjectID)
	if err != nil {
		return convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	rows := make([]map[string]any, 0, len(services))
	for _, s := range services {
		rows = append(rows, map[string]any{
			"hostname": s.Name,
			"id":       s.ID,
			"type":     s.ServiceStackTypeInfo.ServiceStackTypeVersionName,
			"status":   s.Status,
		})
	}
	return jsonResult(map[string]any{
		"workflow":        workflowLaunchProduction,
		"prodOperation":   prodOpStatus,
		"targetProjectId": state.TargetProjectID,
		"launchStatus":    state.Status,
		"services":        rows,
		"pipeline":        state.PipelineConfigurations,
		"doneBoundary":    prodOpsDoneBoundary(state),
	})
}

// prodOpsDoneBoundary renders the bring-up "done" verdict: when imports
// are terminal AND CD is configured/observed (or explicitly skipped),
// the window closes and the ONLY remaining step is key revocation.
func prodOpsDoneBoundary(state *launchState) map[string]any {
	pending := pendingPipelineConfigurations(state)
	done := state.Status == topology.LaunchStatusLaunched && !pending
	next := "Bring-up still in progress — keep the launch key until the pipeline check passes or you explicitly skip it (skipPipelineSetup)."
	if done {
		next = "DONE: production is live and CD is configured/acknowledged. Revoke the launch-window key NOW (Settings → Access Tokens Management) — this closes the bring-up window; further management belongs to the dashboard or a project-scoped token."
	}
	return map[string]any{
		"done":     done,
		"nextStep": next,
	}
}

func prodOpsLogs(ctx context.Context, admin platform.ProjectAdminClient, logFetcher platform.LogFetcher, state *launchState, input WorkflowInput) *mcp.CallToolResult {
	if logFetcher == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented, "log fetcher unavailable in this runtime", ""))
	}
	access, err := admin.GetProjectLogAccess(ctx, state.TargetProjectID)
	if err != nil {
		return convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	params := platform.LogFetchParams{Limit: 100}
	if input.TargetService != "" {
		svc, blockResp := prodOpsResolveService(ctx, admin, state, input.TargetService)
		if blockResp != nil {
			return blockResp
		}
		params.ServiceID = svc.ID
	}
	entries, err := logFetcher.FetchLogs(ctx, access, params)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"workflow":        workflowLaunchProduction,
		"prodOperation":   prodOpLogs,
		"targetProjectId": state.TargetProjectID,
		"entryCount":      len(entries),
		"entries":         entries,
	})
}

func prodOpsEnvKeys(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, input WorkflowInput) *mcp.CallToolResult {
	body := map[string]any{
		"workflow":        workflowLaunchProduction,
		"prodOperation":   prodOpEnvKeys,
		"targetProjectId": state.TargetProjectID,
		"note":            "Key PRESENCE only — values are never read by ZCP (P-LP-5). Set/inspect values in the Zerops dashboard.",
	}
	projectKeys, err := admin.GetProjectEnvKeys(ctx, state.TargetProjectID)
	if err != nil {
		return convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	body["projectEnvKeys"] = projectKeys
	if input.TargetService != "" {
		svc, blockResp := prodOpsResolveService(ctx, admin, state, input.TargetService)
		if blockResp != nil {
			return blockResp
		}
		serviceKeys, envErr := admin.GetServiceEnvKeys(ctx, svc.ID)
		if envErr != nil {
			return convertError(prodOpsTranslateErr(envErr, state), WithRecoveryStatus())
		}
		body["serviceEnvKeys"] = serviceKeys
	}
	return jsonResult(body)
}

func prodOpsLifecycle(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, input WorkflowInput, op string) *mcp.CallToolResult {
	svc, blockResp := prodOpsResolveService(ctx, admin, state, input.TargetService)
	if blockResp != nil {
		return blockResp
	}
	var proc *platform.Process
	var err error
	switch op {
	case prodOpRestart:
		proc, err = admin.RestartService(ctx, svc.ID)
	case prodOpStop:
		proc, err = admin.StopService(ctx, svc.ID)
	case prodOpStart:
		proc, err = admin.StartService(ctx, svc.ID)
	}
	if err != nil {
		return convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"workflow":      workflowLaunchProduction,
		"prodOperation": op,
		"service":       svc.Name,
		"processId":     processIDOf(proc),
		"nextStep":      "Poll with prodOperation=\"status\" (the process is async).",
	})
}

func prodOpsDeleteService(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, input WorkflowInput) *mcp.CallToolResult {
	svc, blockResp := prodOpsResolveService(ctx, admin, state, input.TargetService)
	if blockResp != nil {
		return blockResp
	}
	// Destructive: require the structured ack naming the target —
	// same diagnose-before-destruct convention as import override.
	if input.ConfirmDestructive == nil || !ackCoversTarget(input.ConfirmDestructive, svc.Name) {
		return jsonResult(map[string]any{
			"workflow":      workflowLaunchProduction,
			"prodOperation": prodOpDeleteService,
			"refused":       true,
			"wouldDestroy": map[string]any{
				"services": []map[string]any{{
					"hostname": svc.Name,
					"id":       svc.ID,
					"status":   svc.Status,
				}},
			},
			"retryCall": map[string]any{
				"tool": "zerops_workflow",
				"args": map[string]any{
					"action":                "prod-ops",
					"prodOperation":         "delete-service",
					"productionProjectName": input.ProductionProjectName,
					"targetService":         svc.Name,
					"confirmDestructive": map[string]any{
						"operation":           "prod-delete-service",
						"acknowledgedTargets": []string{svc.Name},
					},
					"launchKey": "<re-supply the launch key>",
				},
			},
			"note": "Deleting a production service destroys its containers + data. Confirm with the user, then re-call with the prefilled ack.",
		})
	}
	proc, err := admin.DeleteService(ctx, svc.ID)
	if err != nil {
		return convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"workflow":      workflowLaunchProduction,
		"prodOperation": prodOpDeleteService,
		"deleted":       svc.Name,
		"processId":     processIDOf(proc),
		"nextStep":      "Poll with prodOperation=\"status\". Re-import a corrected replacement via the launch resume or the dashboard.",
	})
}

// ackCoversTarget reports whether the structured destructive ack names
// the operation + the target hostname.
func ackCoversTarget(ack *DestructiveAck, hostname string) bool {
	if ack.Operation != "prod-delete-service" {
		return false
	}
	return slices.Contains(ack.AcknowledgedTargets, hostname)
}

func processIDOf(p *platform.Process) string {
	if p == nil {
		return ""
	}
	return p.ID
}

// prodOpsScale adjusts a prod service's container range during the
// bring-up window (gap plan P2.5 — the F7 plan listed scale; the shipped
// op set silently dropped it, leaving the dashboard or a fresh
// prod-scoped MCP session as the only post-launch adjustment paths).
// Non-destructive; per-call launchKey like every prod-ops call.
func prodOpsScale(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, input WorkflowInput) *mcp.CallToolResult {
	if input.TargetService == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"prod-ops scale requires targetService (the PROD hostname)",
			"Pass targetService=<prod hostname> plus minContainers/maxContainers via runtimeScaling={\"<host>\":{\"minContainers\":N,\"maxContainers\":M}}."), WithRecoveryStatus())
	}
	scaling, ok := input.RuntimeScaling[input.TargetService]
	if !ok || (scaling.MinContainers == 0 && scaling.MaxContainers == 0) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("prod-ops scale: no container range given for %q", input.TargetService),
			"Pass runtimeScaling={\"<host>\":{\"minContainers\":N,\"maxContainers\":M}} — at least one bound required."), WithRecoveryStatus())
	}
	svc, errResult := prodOpsResolveService(ctx, admin, state, input.TargetService)
	if errResult != nil {
		return errResult
	}
	// ServiceMode must echo the live mode — the platform rejects a
	// scaling PUT whose mode field differs ("mode update forbidden"),
	// same rule the source-project zerops_scale path follows.
	params := platform.AutoscalingParams{ServiceMode: svc.Mode}
	if scaling.MinContainers > 0 {
		v := int32(scaling.MinContainers)
		params.HorizontalMinCount = &v
	}
	if scaling.MaxContainers > 0 {
		v := int32(scaling.MaxContainers)
		params.HorizontalMaxCount = &v
	}
	proc, err := admin.SetServiceScaling(ctx, svc.ID, params)
	if err != nil {
		return convertError(prodOpsTranslateErr(err, state), WithRecoveryStatus())
	}
	resp := map[string]any{
		"workflow":      workflowLaunchProduction,
		"prodOperation": prodOpScale,
		"service":       input.TargetService,
		"scaling":       scaling,
		"nextStep":      "Scaling change submitted. Poll with prodOperation=\"status\".",
	}
	if proc != nil {
		resp["processId"] = proc.ID
	}
	return jsonResult(resp)
}
