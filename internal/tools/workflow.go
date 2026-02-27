package tools

import (
	"context"
	"fmt"
	"strings"
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
	Action         string                    `json:"action,omitempty"         jsonschema:"Orchestration action: start, complete, skip, status, transition, evidence, reset, or iterate."`
	Mode           string                    `json:"mode,omitempty"           jsonschema:"Session mode for start action: full (all phases), dev_only (skip deploy/verify), hotfix (skip discover), or quick (no gates)."`
	Phase          string                    `json:"phase,omitempty"          jsonschema:"Target phase for transition action: DISCOVER, DEVELOP, DEPLOY, VERIFY, or DONE."`
	Intent         string                    `json:"intent,omitempty"         jsonschema:"User intent description for start action (what you want to accomplish)."`
	Type           string                    `json:"type,omitempty"           jsonschema:"Evidence type for evidence action: recipe_review, discovery, dev_verify, deploy_evidence, or stage_verify."`
	Service        string                    `json:"service,omitempty"        jsonschema:"Service hostname to associate with evidence."`
	Attestation    string                    `json:"attestation,omitempty"    jsonschema:"Description of what was verified or accomplished (required for evidence and complete actions)."`
	Step           string                    `json:"step,omitempty"           jsonschema:"Bootstrap step name for complete/skip actions (e.g. detect, mount-dev, discover-envs, deploy)."`
	Plan           []workflow.PlannedService `json:"plan,omitempty"           jsonschema:"Structured service plan for the 'plan' step: array of {hostname, type, mode?}. Validates hostnames and types."`
	Reason         string                    `json:"reason,omitempty"         jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	Passed         *int                      `json:"passed,omitempty"         jsonschema:"Number of passed verifications (evidence action). Defaults to 1."`
	Failed         *int                      `json:"failed,omitempty"         jsonschema:"Number of failed verifications (evidence action). Defaults to 0."`
	ServiceResults []workflow.ServiceResult  `json:"serviceResults,omitempty" jsonschema:"Per-service verification results (evidence action)."`
}

// startResponse wraps WorkflowState with workflow guidance for non-bootstrap start.
type startResponse struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
	Intent    string `json:"intent"`
	Phase     string `json:"phase"`
	Guidance  string `json:"guidance,omitempty"`
}

// RegisterWorkflow registers the zerops_workflow tool.
func RegisterWorkflow(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, engine *workflow.Engine, tracker *ops.KnowledgeTracker) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" mode=\"full|dev_only|hotfix|quick\" to begin a tracked session with guidance. mode is REQUIRED. Workflows: bootstrap, deploy, debug, scale, configure. After start: action=\"complete|skip|status\" (bootstrap steps), action=\"transition|evidence|reset|iterate\" (phase management).",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, cache, tracker, input)
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

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, tracker *ops.KnowledgeTracker, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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
		return handleBootstrapComplete(engine, input, liveTypes, tracker)
	case "skip":
		return handleBootstrapSkip(engine, input)
	case "status":
		return handleBootstrapStatus(engine, tracker)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, complete, skip, status, transition, evidence, reset, iterate")), nil, nil
	}
}

func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Mode == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Mode is required for start action",
			"Specify mode: full, dev_only, hotfix, or quick")), nil, nil
	}

	mode := workflow.Mode(input.Mode)

	// Bootstrap conductor: use BootstrapStart for bootstrap workflow.
	if input.Workflow == workflowBootstrap {
		resp, err := engine.BootstrapStart(projectID, mode, input.Intent)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrWorkflowActive,
				fmt.Sprintf("Bootstrap start failed: %v", err),
				"Reset existing session first with action=reset")), nil, nil
		}
		return jsonResult(resp), nil, nil
	}

	// Generic start (non-bootstrap or quick mode).
	wfName := input.Workflow
	if wfName == "" {
		wfName = "workflow"
	}
	state, err := engine.Start(projectID, wfName, mode, input.Intent)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Start failed: %v", err),
			"Reset existing session first with action=reset")), nil, nil
	}

	resp := startResponse{
		SessionID: state.SessionID,
		Mode:      string(state.Mode),
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

func handleTransition(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Phase == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Phase is required for transition action",
			"Specify phase: DISCOVER, DEVELOP, DEPLOY, VERIFY, or DONE")), nil, nil
	}

	phase := workflow.Phase(input.Phase)
	state, err := engine.Transition(phase)
	if err != nil {
		if strings.Contains(err.Error(), "gate") {
			return convertError(platform.NewPlatformError(
				platform.ErrGateFailed,
				err.Error(),
				"Record required evidence before transitioning")), nil, nil
		}
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Transition failed: %v", err),
			"Start a session first with action=start")), nil, nil
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

func handleBootstrapComplete(engine *workflow.Engine, input WorkflowInput, liveTypes []platform.ServiceStackType, tracker *ops.KnowledgeTracker) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for complete action",
			"Specify step name (e.g., step=\"detect\")")), nil, nil
	}

	// Structured plan routing for "plan" step.
	if input.Step == "plan" && len(input.Plan) > 0 {
		resp, err := engine.BootstrapCompletePlan(input.Plan, liveTypes)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Plan validation failed: %v", err),
				"Provide valid plan: [{hostname, type, mode?}]. Hostnames: lowercase a-z0-9, max 25 chars. Managed services default to mode: NON_HA. Specify HA explicitly for production.")), nil, nil
		}
		injectKnowledgeHint(resp, tracker)
		return jsonResult(resp), nil, nil
	}

	// Default: free-text attestation.
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for complete action",
			"Describe what was accomplished in this step")), nil, nil
	}

	resp, err := engine.BootstrapComplete(input.Step, input.Attestation)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Complete step failed: %v", err),
			"Start bootstrap first with action=start workflow=bootstrap")), nil, nil
	}
	injectKnowledgeHint(resp, tracker)
	return jsonResult(resp), nil, nil
}

func handleBootstrapSkip(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for skip action",
			"Specify step name (e.g., step=\"mount-dev\")")), nil, nil
	}

	reason := input.Reason
	if reason == "" {
		reason = "skipped by user"
	}

	resp, err := engine.BootstrapSkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Skip step failed: %v", err),
			"Only skippable steps (mount-dev, discover-envs, deploy) can be skipped")), nil, nil
	}
	return jsonResult(resp), nil, nil
}

func handleBootstrapStatus(engine *workflow.Engine, tracker *ops.KnowledgeTracker) (*mcp.CallToolResult, any, error) {
	resp, err := engine.BootstrapStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Bootstrap status failed: %v", err),
			"Start bootstrap first with action=start workflow=bootstrap")), nil, nil
	}
	injectKnowledgeHint(resp, tracker)
	return jsonResult(resp), nil, nil
}

// injectKnowledgeHint adds a hint to the load-knowledge step guidance when
// knowledge has already been loaded via prior zerops_knowledge calls.
func injectKnowledgeHint(resp *workflow.BootstrapResponse, tracker *ops.KnowledgeTracker) {
	if resp.Current == nil || resp.Current.Name != "load-knowledge" {
		return
	}
	if tracker == nil || !tracker.IsLoaded() {
		return
	}
	resp.Current.Guidance = fmt.Sprintf(
		"Knowledge already loaded (%s).\nComplete this step with: zerops_workflow action=\"complete\" step=\"load-knowledge\" attestation=\"Already loaded\"",
		tracker.Summary(),
	)
}

// injectStacks inserts the stack list section into workflow content.
// Replaces content between STACKS markers if present, otherwise inserts before "## Phase 1".
func injectStacks(content, stackList string) string {
	const beginMarker = "<!-- STACKS:BEGIN -->"
	const endMarker = "<!-- STACKS:END -->"

	if beginIdx := strings.Index(content, beginMarker); beginIdx >= 0 {
		if endIdx := strings.Index(content, endMarker); endIdx > beginIdx {
			return content[:beginIdx] + stackList + content[endIdx+len(endMarker):]
		}
	}

	// Fallback: insert before "## Phase 1"
	const anchor = "## Phase 1"
	if idx := strings.Index(content, anchor); idx > 0 {
		return content[:idx] + stackList + "\n---\n\n" + content[idx:]
	}

	return content
}
