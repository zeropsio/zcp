package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	workflowBootstrap = workflow.WorkflowBootstrap
	workflowDeploy    = workflow.WorkflowDeploy
	workflowCICD      = workflow.WorkflowCICD
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	// Legacy: workflow name for static guidance (backward compat).
	Workflow string `json:"workflow,omitempty" jsonschema:"Workflow name: bootstrap, deploy, debug, configure, or cicd."`

	// Multi-action fields.
	Action      string                     `json:"action,omitempty"      jsonschema:"Orchestration action: start, complete, skip, status, reset, iterate, resume, list, or route."`
	Intent      string                     `json:"intent,omitempty"      jsonschema:"User intent description for start action (what you want to accomplish)."`
	Attestation string                     `json:"attestation,omitempty" jsonschema:"Description of what was verified or accomplished (required for complete actions)."`
	Step        string                     `json:"step,omitempty"        jsonschema:"Bootstrap step name for complete/skip actions (e.g. discover, provision, generate, deploy, close)."`
	Plan        []workflow.BootstrapTarget `json:"plan,omitempty"        jsonschema:"Structured service plan: array of {runtime: {devHostname, type, bootstrapMode?}, dependencies: [{hostname, type, mode?, resolution}]}. resolution: CREATE (new service), EXISTS (already in project), SHARED (created by another target in this plan)."`
	Reason      string                     `json:"reason,omitempty"      jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	SessionID   string                     `json:"sessionId,omitempty"   jsonschema:"Session ID for resume action."`
	Strategies  map[string]string          `json:"strategies,omitempty"  jsonschema:"Per-service strategy map for strategy action (e.g. {\"appdev\":\"ci-cd\"})."`
}

// immediateResponse is returned from immediate (stateless) workflows.
type immediateResponse struct {
	Workflow string `json:"workflow"`
	Guidance string `json:"guidance"`
}

// RegisterWorkflow registers the zerops_workflow tool.
// selfHostname is the hostname of the service running ZCP (empty when local).
func RegisterWorkflow(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, engine *workflow.Engine, logFetcher platform.LogFetcher, stateDir, selfHostname string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" to begin a tracked session with guidance. Workflows: bootstrap, deploy, debug, configure, cicd. After start: action=\"complete|skip|status\" (bootstrap steps), action=\"reset|iterate|resume|list|route\".",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, cache, logFetcher, input, stateDir, selfHostname)
		}

		// Legacy: static workflow guidance.
		if input.Workflow == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No workflow specified",
				"Use workflow=\"bootstrap\", workflow=\"deploy\", or workflow=\"debug\"")), nil, nil
		}
		wfContent, err := content.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(err), nil, nil
		}

		// Inject live stack types into bootstrap/deploy workflows.
		if (input.Workflow == workflowBootstrap || input.Workflow == workflowDeploy) && client != nil && cache != nil {
			if types := cache.Get(ctx, client); len(types) > 0 {
				stackList := knowledge.FormatStackList(types)
				wfContent = injectStacks(wfContent, stackList)
			}
		}

		return textResult(wfContent), nil, nil
	})
}

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, logFetcher platform.LogFetcher, input WorkflowInput, stateDir, selfHostname string) (*mcp.CallToolResult, any, error) {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory")), nil, nil
	}

	switch input.Action {
	case "start":
		return handleStart(ctx, projectID, engine, client, cache, input)
	case "reset":
		return handleReset(engine)
	case "iterate":
		return handleIterate(ctx, engine, client, cache)
	case "complete":
		// Route to the active workflow type.
		switch detectActiveWorkflow(engine) {
		case workflowDeploy:
			return handleDeployComplete(ctx, engine, client, projectID, stateDir, input)
		case workflowCICD:
			return handleCICDComplete(ctx, engine, input)
		}
		var liveTypes []platform.ServiceStackType
		if cache != nil && client != nil {
			liveTypes = cache.Get(ctx, client)
		}
		return handleBootstrapComplete(ctx, engine, client, cache, input, liveTypes, logFetcher, projectID, stateDir)
	case "skip":
		if detectActiveWorkflow(engine) == workflowDeploy {
			return handleDeploySkip(ctx, engine, input)
		}
		return handleBootstrapSkip(ctx, engine, client, cache, input)
	case "status":
		switch detectActiveWorkflow(engine) {
		case workflowDeploy:
			return handleDeployStatus(ctx, engine)
		case workflowCICD:
			return handleCICDStatus(ctx, engine)
		}
		return handleBootstrapStatus(ctx, engine, client, cache)
	case "resume":
		return handleResume(ctx, engine, client, cache, input)
	case "list":
		return handleListSessions(engine)
	case "route":
		return handleRoute(ctx, engine, client, projectID, stateDir, selfHostname)
	case "strategy":
		return handleStrategy(engine, input, stateDir)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, complete, skip, status, reset, iterate, resume, list, route, strategy")), nil, nil
	}
}

func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	// Immediate workflows: stateless, return guidance directly.
	if workflow.IsImmediateWorkflow(input.Workflow) {
		wfContent, err := content.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Workflow %q not found: %v", input.Workflow, err),
				"Valid workflows: bootstrap, deploy, debug, configure, cicd")), nil, nil
		}
		return jsonResult(immediateResponse{
			Workflow: input.Workflow,
			Guidance: wfContent,
		}), nil, nil
	}

	// Bootstrap conductor.
	if input.Workflow == workflowBootstrap {
		resp, err := engine.BootstrapStart(projectID, input.Intent)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrWorkflowActive,
				fmt.Sprintf("Bootstrap start failed: %v", err),
				"Reset existing session first with action=reset")), nil, nil
		}
		populateStacks(ctx, resp, client, cache)
		return jsonResult(resp), nil, nil
	}

	// Deploy workflow.
	if input.Workflow == workflowDeploy {
		return handleDeployStart(ctx, engine, client, projectID, input)
	}

	// CI/CD workflow.
	if input.Workflow == workflowCICD {
		return handleCICDStart(ctx, engine, projectID, input)
	}

	// Unknown workflow — return error.
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("Unknown orchestrated workflow %q", input.Workflow),
		"Valid workflows: bootstrap, deploy, debug, configure, cicd")), nil, nil
}

// detectActiveWorkflow returns the active workflow type from engine state.
func detectActiveWorkflow(engine *workflow.Engine) string {
	if !engine.HasActiveSession() {
		return ""
	}
	state, err := engine.GetState()
	if err != nil {
		return ""
	}
	if state.Deploy != nil && state.Deploy.Active {
		return workflowDeploy
	}
	if state.CICD != nil && state.CICD.Active {
		return workflowCICD
	}
	return workflowBootstrap
}

func handleReset(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	if err := engine.Reset(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Reset failed: %v", err),
			"")), nil, nil
	}
	return textResult("Session reset successfully."), nil, nil
}

func handleIterate(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache) (*mcp.CallToolResult, any, error) {
	if _, err := engine.Iterate(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Iterate failed: %v", err),
			"Start a session first")), nil, nil
	}
	// Route to the right status handler after iteration.
	switch detectActiveWorkflow(engine) {
	case workflowDeploy:
		return handleDeployStatus(ctx, engine)
	case workflowCICD:
		return handleCICDStatus(ctx, engine)
	}
	return bootstrapStatusResult(ctx, engine, client, cache)
}

func handleResume(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.SessionID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"sessionId is required for resume action",
			"Specify the session ID to resume")), nil, nil
	}
	if _, err := engine.Resume(input.SessionID); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Resume failed: %v", err),
			"Session may not exist or may still be active")), nil, nil
	}
	switch detectActiveWorkflow(engine) {
	case workflowDeploy:
		return handleDeployStatus(ctx, engine)
	case workflowCICD:
		return handleCICDStatus(ctx, engine)
	}
	return bootstrapStatusResult(ctx, engine, client, cache)
}

func handleListSessions(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	sessions, err := engine.ListActiveSessions()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("List sessions failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(sessions), nil, nil
}
