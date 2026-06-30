package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// ProcessInput is the input type for zerops_process.
type ProcessInput struct {
	ProcessID  string   `json:"processId,omitempty"  jsonschema:"Process ID — for action status (default), cancel, or wait."`
	ProcessIDs []string `json:"processIds,omitempty" jsonschema:"Process IDs to wait on together (action=wait)."`
	Service    string   `json:"service,omitempty"    jsonschema:"Hostname to wait on (action=wait): blocks until the service has no live process — drains build, deploy, and queued ops. Preferred 'wait until ready' form."`
	Action     string   `json:"action,omitempty"     jsonschema:"status (default) | wait | cancel."`
}

// RegisterProcess registers the zerops_process tool.
func RegisterProcess(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_process",
		Description: "Check status, wait for, or cancel async work. Mutating tools poll automatically, so use this to: WAIT for in-flight work to finish (action=\"wait\" — pass service=<hostname> to block until that service has no live process, draining build+deploy+any queued op like subdomain-enable; or processId/processIds to wait on specific process(es)); CANCEL a running process (action=\"cancel\"); or CHECK a historical one (action=\"status\", default).",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Wait for, check, or cancel async process",
			IdempotentHint:  true,
			DestructiveHint: boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ProcessInput) (*mcp.CallToolResult, any, error) {
		action := input.Action
		if action == "" {
			action = actionStatus
		}

		switch action {
		case actionStatus:
			result, err := ops.GetProcessStatus(ctx, client, input.ProcessID)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "wait":
			// Progress notifications keep the MCP connection alive across a
			// multi-minute build wait (same mechanism the deploy/import polls use);
			// PollProcess emits them only one interval before any response, so the
			// documented progress/response race never triggers.
			onProgress := buildProgressCallback(ctx, req)
			var (
				result *ops.WaitResult
				err    error
			)
			if input.Service != "" {
				result, err = ops.WaitServiceSettled(ctx, client, projectID, input.Service, onProgress)
			} else {
				ids := input.ProcessIDs
				if input.ProcessID != "" {
					ids = append(ids, input.ProcessID)
				}
				result, err = ops.WaitProcesses(ctx, client, ids, onProgress)
			}
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "cancel":
			result, err := ops.CancelProcess(ctx, client, input.ProcessID)
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Invalid action '"+action+"'",
				"Use 'status', 'wait', or 'cancel'",
			)), nil, nil
		}
	})
}
