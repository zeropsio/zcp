package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	workflowBootstrap = "bootstrap"
	workflowDeploy    = "deploy"
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	// Legacy: workflow name for static guidance (backward compat).
	Workflow string `json:"workflow,omitempty" jsonschema:"Workflow name for static guidance: bootstrap, deploy, debug, scale, or configure."`

	// Multi-action fields.
	Action         string                     `json:"action,omitempty"         jsonschema:"Orchestration action: start, complete, skip, status, transition, evidence, reset, iterate, or resume."`
	Phase          string                     `json:"phase,omitempty"          jsonschema:"Target phase for transition action: DISCOVER, DEVELOP, DEPLOY, VERIFY, or DONE."`
	Intent         string                     `json:"intent,omitempty"         jsonschema:"User intent description for start action (what you want to accomplish)."`
	Type           string                     `json:"type,omitempty"           jsonschema:"Evidence type for evidence action: recipe_review, discovery, dev_verify, deploy_evidence, or stage_verify."`
	Service        string                     `json:"service,omitempty"        jsonschema:"Service hostname to associate with evidence."`
	Attestation    string                     `json:"attestation,omitempty"    jsonschema:"Description of what was verified or accomplished (required for evidence and complete actions)."`
	Step           string                     `json:"step,omitempty"           jsonschema:"Bootstrap step name for complete/skip actions (e.g. discover, provision, generate, deploy, verify)."`
	Plan           []workflow.BootstrapTarget `json:"plan,omitempty"           jsonschema:"Structured service plan: array of {runtime: {devHostname, type}, dependencies: [{hostname, type, mode?, resolution?}]}."`
	Reason         string                     `json:"reason,omitempty"         jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	Passed         *int                       `json:"passed,omitempty"         jsonschema:"Number of passed verifications (evidence action). Defaults to 1."`
	Failed         *int                       `json:"failed,omitempty"         jsonschema:"Number of failed verifications (evidence action). Defaults to 0."`
	ServiceResults []workflow.ServiceResult   `json:"serviceResults,omitempty" jsonschema:"Per-service verification results (evidence action)."`
	SessionID      string                     `json:"sessionId,omitempty"      jsonschema:"Session ID for resume action."`
	Strategies     map[string]string          `json:"strategies,omitempty"     jsonschema:"Per-service strategy map for strategy step completion (e.g. {\"appdev\":\"ci-cd\"})."`
}

// startResponse wraps WorkflowState with workflow guidance for non-bootstrap orchestrated start.
type startResponse struct {
	SessionID string `json:"sessionId"`
	Intent    string `json:"intent"`
	Phase     string `json:"phase"`
	Guidance  string `json:"guidance,omitempty"`
}

// immediateResponse is returned from immediate (stateless) workflows.
type immediateResponse struct {
	Workflow string `json:"workflow"`
	Guidance string `json:"guidance"`
}

// RegisterWorkflow registers the zerops_workflow tool.
func RegisterWorkflow(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, engine *workflow.Engine, logFetcher platform.LogFetcher, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" to begin a tracked session with guidance. Workflows: bootstrap, deploy, debug, scale, configure. After start: action=\"complete|skip|status\" (bootstrap steps), action=\"transition|evidence|reset|iterate|resume|list\" (phase management).",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, cache, logFetcher, input, stateDir)
		}

		// Legacy: static workflow guidance.
		if input.Workflow == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No workflow specified",
				"Use workflow=\"bootstrap\", workflow=\"deploy\", or workflow=\"debug\"")), nil, nil
		}
		content, err := ops.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(err), nil, nil
		}

		// Inject live stack types into bootstrap/deploy workflows.
		if (input.Workflow == workflowBootstrap || input.Workflow == workflowDeploy) && client != nil && cache != nil {
			if types := cache.Get(ctx, client); len(types) > 0 {
				stackList := knowledge.FormatStackList(types)
				content = injectStacks(content, stackList)
			}
		}

		return textResult(content), nil, nil
	})
}

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, logFetcher platform.LogFetcher, input WorkflowInput, stateDir string) (*mcp.CallToolResult, any, error) {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory")), nil, nil
	}

	switch input.Action {
	case "start":
		return handleStart(ctx, projectID, engine, client, cache, input)
	case "transition":
		return handleTransition(engine, input)
	case "evidence":
		return handleEvidence(engine, input)
	case "reset":
		return handleReset(engine)
	case "iterate":
		return handleIterate(engine)
	case "complete":
		var liveTypes []platform.ServiceStackType
		if cache != nil && client != nil {
			liveTypes = cache.Get(ctx, client)
		}
		return handleBootstrapComplete(ctx, engine, client, cache, input, liveTypes, logFetcher, projectID, stateDir)
	case "skip":
		return handleBootstrapSkip(ctx, engine, client, cache, input)
	case "status":
		return handleBootstrapStatus(ctx, engine, client, cache)
	case "resume":
		return handleResume(engine, input)
	case "list":
		return handleListSessions(engine)
	case "route":
		return handleRoute(ctx, engine, client, projectID, stateDir)
	case "strategy":
		return handleStrategy(engine, input, stateDir)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, complete, skip, status, transition, evidence, reset, iterate, resume, list, route, strategy")), nil, nil
	}
}

func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	// Immediate workflows: stateless, return guidance only.
	if workflow.IsImmediateWorkflow(input.Workflow) {
		content, err := ops.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Workflow %q not found: %v", input.Workflow, err),
				"Valid workflows: bootstrap, deploy, debug, scale, configure")), nil, nil
		}
		return jsonResult(immediateResponse{
			Workflow: input.Workflow,
			Guidance: content,
		}), nil, nil
	}

	// Orchestrated workflows: bootstrap and deploy.

	// Bootstrap conductor: use BootstrapStart for bootstrap workflow.
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

	// Generic orchestrated start (deploy or other).
	wfName := input.Workflow
	if wfName == "" {
		wfName = "workflow"
	}
	state, err := engine.Start(projectID, wfName, input.Intent)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Start failed: %v", err),
			"Reset existing session first with action=reset")), nil, nil
	}

	resp := startResponse{
		SessionID: state.SessionID,
		Intent:    state.Intent,
		Phase:     string(state.Phase),
	}
	if input.Workflow != "" {
		if guidance, err := ops.GetWorkflow(input.Workflow); err == nil {
			if (input.Workflow == workflowBootstrap || input.Workflow == workflowDeploy) && client != nil && cache != nil {
				if types := cache.Get(ctx, client); len(types) > 0 {
					guidance = injectStacks(guidance, knowledge.FormatStackList(types))
				}
			}
			resp.Guidance = guidance
		}
	}

	return jsonResult(resp), nil, nil
}

// gateFailureResponse wraps a structured gate failure for JSON output.
type gateFailureResponse struct {
	Status      string                     `json:"status"`
	Gate        string                     `json:"gate"`
	Missing     []string                   `json:"missing,omitempty"`
	Failures    []string                   `json:"failures,omitempty"`
	Remediation []workflow.RemediationStep `json:"remediation,omitempty"`
}

func handleTransition(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Phase == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Phase is required for transition action",
			"Specify phase: DISCOVER, DEVELOP, DEPLOY, VERIFY, or DONE")), nil, nil
	}

	phase := workflow.Phase(input.Phase)
	state, gateResult, err := engine.Transition(phase)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Transition failed: %v", err),
			"Start a session first with action=start")), nil, nil
	}
	if gateResult != nil {
		return jsonResult(gateFailureResponse{
			Status:      "gate_failed",
			Gate:        gateResult.Gate,
			Missing:     gateResult.Missing,
			Failures:    gateResult.Failures,
			Remediation: gateResult.Remediation,
		}), nil, nil
	}

	return jsonResult(state), nil, nil
}

func handleEvidence(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Type == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Type is required for evidence action",
			"Specify type: recipe_review, discovery, dev_verify, deploy_evidence, or stage_verify")), nil, nil
	}
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for evidence action",
			"Describe what was verified")), nil, nil
	}

	passed := 1
	if input.Passed != nil {
		passed = *input.Passed
	}
	failed := 0
	if input.Failed != nil {
		failed = *input.Failed
	}

	ev := &workflow.Evidence{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		VerificationType: "attestation",
		Service:          input.Service,
		Attestation:      input.Attestation,
		Type:             input.Type,
		Passed:           passed,
		Failed:           failed,
		ServiceResults:   input.ServiceResults,
	}

	if err := engine.RecordEvidence(ev); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrEvidenceMissing,
			fmt.Sprintf("Record evidence failed: %v", err),
			"Start a session first with action=start")), nil, nil
	}

	result := map[string]string{
		"status": "recorded",
		"type":   input.Type,
	}
	return jsonResult(result), nil, nil
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

func handleIterate(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	state, err := engine.Iterate()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Iterate failed: %v", err),
			"Start a session first")), nil, nil
	}
	return jsonResult(state), nil, nil
}

func handleResume(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.SessionID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"sessionId is required for resume action",
			"Specify the session ID to resume")), nil, nil
	}
	state, err := engine.Resume(input.SessionID)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Resume failed: %v", err),
			"Session may not exist or may still be active")), nil, nil
	}
	return jsonResult(state), nil, nil
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
