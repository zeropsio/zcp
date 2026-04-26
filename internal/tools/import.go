package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ImportInput is the input type for zerops_import.
//
// Override is FlexBool so the MCP schema accepts both booleans and
// stringified forms — same rationale as every other MCP-boundary
// boolean input (some LLM agents serialize primitives as quoted
// strings, and the raw-bool schema would reject those at the protocol
// layer with a non-actionable error).
type ImportInput struct {
	Content  string   `json:"content,omitempty"`
	FilePath string   `json:"filePath,omitempty"`
	Override FlexBool `json:"override,omitempty"`
}

// importInputSchema is the explicit InputSchema for zerops_import. Lives
// here rather than on struct tags so `override` can declare the
// `oneOf: [boolean, string]` shape needed by stringified-boolean agents.
func importInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"content": {
			Type:        "string",
			Description: "Inline import YAML content. Provide either content or filePath.",
		},
		"filePath": {
			Type:        "string",
			Description: "Path to a YAML file containing the import definition. Provide either filePath or content.",
		},
		"override": flexBoolSchema("Set override: true on every imported service so the API replaces existing service stacks with matching hostnames. Required when re-importing a service that already exists (e.g. to transition READY_TO_DEPLOY to ACTIVE by adding startWithoutCode: true)."),
	})
}

// RegisterImport registers the zerops_import tool.
//
// No longer takes StackTypeCache / schema.Cache — the Zerops API is now
// the single validator for everything the import YAML declares. Field /
// mode / type errors come back with structured apiMeta via the error
// surface established by the validation-plumbing plan.
func RegisterImport(srv *mcp.Server, client platform.Client, projectID string, engine *workflow.Engine, stateDir string, recipeProbe RecipeSessionProbe) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_import",
		Description: "REQUIRES active workflow (zerops_recipe for recipe authoring, or zerops_workflow bootstrap/develop). Import services from YAML into the project. The Zerops API validates fields, modes, types, and hostnames server-side and returns structured apiMeta on the error response when anything is wrong. Blocks until all processes complete; returns final statuses (FINISHED/FAILED).",
		InputSchema: importInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Import services from YAML",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ImportInput) (*mcp.CallToolResult, any, error) {
		if blocked := requireWorkflowContext(engine, stateDir, recipeProbe); blocked != nil {
			return blocked, nil, nil
		}
		result, err := ops.Import(ctx, client, projectID, input.Content, input.FilePath, input.Override.Bool())
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollImportProcesses(ctx, client, result, onProgress)

		return jsonResult(result), nil, nil
	})
}

// pollImportProcesses polls each import process until completion, updating
// the result's process statuses and summary in-place.
func pollImportProcesses(
	ctx context.Context,
	client platform.Client,
	result *ops.ImportResult,
	onProgress ops.ProgressCallback,
) {
	finished := 0
	failed := 0
	for i := range result.Processes {
		proc := &result.Processes[i]
		if proc.ProcessID == "" {
			continue
		}
		finalProc, err := ops.PollProcess(ctx, client, proc.ProcessID, onProgress)
		if err != nil {
			// On timeout/error, keep original status.
			continue
		}
		proc.Status = finalProc.Status
		proc.FailReason = finalProc.FailReason
		switch finalProc.Status {
		case statusFinished:
			finished++
		case statusFailed:
			failed++
		}
	}

	total := len(result.Processes)
	if total == 0 {
		return
	}
	if failed > 0 {
		result.Summary = fmt.Sprintf("%d/%d processes completed, %d failed", finished, total, failed)
		result.NextActions = nextActionImportPartial
	} else {
		result.Summary = fmt.Sprintf("All %d processes completed successfully", total)
		result.NextActions = nextActionImportSuccess
	}
}
