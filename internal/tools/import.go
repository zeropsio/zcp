package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ImportInput is the input type for zerops_import.
type ImportInput struct {
	Content  string `json:"content,omitempty"  jsonschema:"Inline import YAML content. Provide either content or filePath."`
	FilePath string `json:"filePath,omitempty" jsonschema:"Path to a YAML file containing the import definition. Provide either filePath or content."`
}

// RegisterImport registers the zerops_import tool.
func RegisterImport(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_import",
		Description: "REQUIRES active workflow session — call zerops_workflow action=\"start\" first. Import services from YAML into the current project. Validates service types before calling the API. Blocks until all processes complete — returns final statuses (FINISHED/FAILED). NOTE: enableSubdomainAccess=true in import YAML pre-configures routing but does NOT activate it. You MUST call zerops_subdomain action=\"enable\" after the first successful deploy to activate routing and get subdomain URLs.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Import services from YAML",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ImportInput) (*mcp.CallToolResult, any, error) {
		if blocked := requireWorkflow(engine); blocked != nil {
			return blocked, nil, nil
		}
		var liveTypes []platform.ServiceStackType
		if cache != nil {
			liveTypes = cache.Get(ctx, client)
		}
		result, err := ops.Import(ctx, client, projectID, input.Content, input.FilePath, liveTypes)
		if err != nil {
			return convertError(err), nil, nil
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
