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

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	// Legacy: workflow name for static guidance (backward compat).
	Workflow string `json:"workflow,omitempty"`

	// New multi-action fields.
	Action      string `json:"action,omitempty"`      // start, transition, reset, evidence, iterate
	Mode        string `json:"mode,omitempty"`        // full, dev_only, hotfix, quick
	Phase       string `json:"phase,omitempty"`       // target phase for transition
	Intent      string `json:"intent,omitempty"`      // user intent for start
	Type        string `json:"type,omitempty"`        // evidence type
	Service     string `json:"service,omitempty"`     // service for evidence
	Attestation string `json:"attestation,omitempty"` // attestation text for evidence
}

// RegisterWorkflow registers the zerops_workflow tool.
func RegisterWorkflow(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Get step-by-step workflow for multi-step operations. Includes live service versions and orchestration steps. Requires workflow parameter: bootstrap, deploy, debug, scale, configure, or monitor.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Get workflow guidance",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(projectID, engine, input)
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
		if (input.Workflow == "bootstrap" || input.Workflow == "deploy") && client != nil && cache != nil {
			if types := cache.Get(ctx, client); len(types) > 0 {
				stackList := knowledge.FormatStackList(types)
				content = injectStacks(content, stackList)
			}
		}

		return textResult(content), nil, nil
	})
}

func handleWorkflowAction(projectID string, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory")), nil, nil
	}

	switch input.Action {
	case "start":
		return handleStart(projectID, engine, input)
	case "transition":
		return handleTransition(engine, input)
	case "evidence":
		return handleEvidence(engine, input)
	case "reset":
		return handleReset(engine)
	case "iterate":
		return handleIterate(engine)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, transition, evidence, reset, iterate")), nil, nil
	}
}

func handleStart(projectID string, engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Mode == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Mode is required for start action",
			"Specify mode: full, dev_only, hotfix, or quick")), nil, nil
	}

	mode := workflow.Mode(input.Mode)
	state, err := engine.Start(projectID, mode, input.Intent)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Start failed: %v", err),
			"Reset existing session first with action=reset")), nil, nil
	}

	return jsonResult(state), nil, nil
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

	ev := &workflow.Evidence{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		VerificationType: "attestation",
		Service:          input.Service,
		Attestation:      input.Attestation,
		Type:             input.Type,
		Passed:           1,
		Failed:           0,
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
