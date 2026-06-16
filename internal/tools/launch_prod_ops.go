package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// F7 — the bring-up management window. While a launched (or failed)
// production project is being brought up, the operator can manage it
// THROUGH ZCP over the public REST API: list services, read logs, read
// env keys, restart/stop/start a service, delete a botched service.
// Every call resolves the launch-window token fresh (P-LP-1: never
// persisted; client constructed + Closed per call): from the staged
// ZCP_LAUNCH_TOKEN service secret on the source push service
// (single-token lifecycle T2 — the agent does NOT re-send the value),
// with an explicit per-call launchKey accepted as fallback when the
// staged secret is gone. The non-secret target identity
// (TargetProjectID, service handles) comes from the persisted launch
// state. SSH/VPN surfaces stay out of scope — the public API covers
// every operation here.
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
// productionProjectName (locates the launch state) + prodOperation;
// service-targeting ops also need targetService (the PROD hostname).
// The launch-window token resolves from the staged secret (launchKey
// only as explicit fallback). client is the SOURCE-project session
// client used for the staged-secret read.
func handleLaunchProdOps(
	ctx context.Context,
	projectID string,
	client platform.Client,
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

	launchKey, stageErr := resolveLaunchWindowToken(ctx, client, projectID, state, input.LaunchKey)
	if stageErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("prod-ops could not READ the staged %s secret on %q: %v", ops.LaunchTokenEnvKey, state.TargetServiceHostname, stageErr),
			"This is a read failure, not an absent token — check the source service is reachable (SSH/VPN), then re-call. Pass launchKey=<token> only if the staged secret is genuinely gone.",
		), WithRecoveryStatus()), nil, nil
	}
	if launchKey == "" {
		msg := fmt.Sprintf("prod-ops could not resolve the launch-window token: no launchKey was passed and the staged %s secret is absent on %q (the token is never persisted — it lives only inside each request)", ops.LaunchTokenEnvKey, state.TargetServiceHostname)
		if !state.WindowClosedAt.IsZero() {
			msg = fmt.Sprintf("the launch window for %q was closed by action=\"confirm-production\" at %s — the staged %s secret is deleted, so prod-ops has no token to work with", input.ProductionProjectName, state.WindowClosedAt.UTC().Format("2006-01-02T15:04:05Z"), ops.LaunchTokenEnvKey)
		}
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			msg,
			`If the launch window was closed (action="confirm-production" deletes the staged secret), production management belongs to the Zerops dashboard or a fresh project-scoped token (existing-project path / a new MCP session against the prod project). If the window should still be open, re-call with launchKey=<the integration token> — ask the user for it, never invent one.`,
		), WithRecoveryStatus()), nil, nil
	}

	admin, adminErr := projectAdminClientFactory(launchKey, apiHost)
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
		return prodOpsStatus(ctx, admin, state, launchDeliveryFamily(stateDir, state)), nil, nil
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

func prodOpsStatus(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, family topology.BuildIntegration) *mcp.CallToolResult {
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
		"doneBoundary":    prodOpsDoneBoundary(state, family),
	})
}

// prodOpsDoneBoundary renders the bring-up "done" verdict: when imports
// are terminal AND CD is configured/observed (or explicitly skipped),
// the remaining step is the explicit window close (confirm-production)
// once the user verifies production works. The actions family treats a
// pending platform integration-status as DONE — GitHub Actions registers
// no Zerops webhook integration, so the entry is expectedly
// not-configured forever (same suppression the launched response's
// pipeline blockers apply; the live lifecycle e2e caught this surface
// holding "bring-up in progress" indefinitely).
func prodOpsDoneBoundary(state *launchState, family topology.BuildIntegration) map[string]any {
	pending := pendingPipelineConfigurations(state) && family != topology.BuildIntegrationActions
	done := state.Status == topology.LaunchStatusLaunched && !pending
	next := "Bring-up still in progress — the launch window stays open (staged " + ops.LaunchTokenEnvKey + " secret) until the pipeline check passes or you explicitly skip it (skipPipelineSetup)."
	switch {
	case !state.WindowClosedAt.IsZero():
		next = "The launch window was closed at " + state.WindowClosedAt.UTC().Format("2006-01-02T15:04:05Z") + " (confirm-production). Further management belongs to the dashboard or a project-scoped token."
	case done:
		next = `DONE: production is live and CD is configured/acknowledged. After the user verifies production is fully functional (first release live + smoke check), close the launch window: zerops_workflow action="confirm-production" productionProjectName="` + state.TargetProjectName + `" confirmFunctional=true.`
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
