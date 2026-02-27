package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EnvInput is the input type for zerops_env.
type EnvInput struct {
	Action          string   `json:"action"                    jsonschema:"Action to perform: set or delete. To read env vars use zerops_discover with includeEnvs=true."`
	ServiceHostname string   `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to modify env vars on. Required unless project=true."`
	Project         bool     `json:"project,omitempty"         jsonschema:"Set to true to manage project-level env vars instead of service-level."`
	Variables       []string `json:"variables,omitempty"       jsonschema:"List of env vars. For set: KEY=VALUE strings. For delete: KEY names only."`
}

// RegisterEnv registers the zerops_env tool.
func RegisterEnv(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_env",
		Description: "Manage environment variables. Actions: set, delete. Scope: service (serviceHostname) or project (project=true). Blocks until the process completes — returns final status (FINISHED/FAILED). After changing env vars, you MUST reload the service (zerops_manage action=reload) for the running application to pick up the new values. Reload is fast (~4s) vs restart (~14s). To read env vars, use zerops_discover with includeEnvs=true.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage environment variables",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
		onProgress := buildProgressCallback(ctx, req)

		switch input.Action {
		case "get":
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Action 'get' is removed — use zerops_discover with includeEnvs=true",
				"zerops_discover returns both service and project env vars")), nil, nil
		case "set":
			result, err := ops.EnvSet(ctx, client, projectID, input.ServiceHostname, input.Project, input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			if result.Process != nil {
				result.Process, _ = pollManageProcess(ctx, client, result.Process, onProgress)
			}
			result.NextActions = nextActionEnvSetSuccess
			return jsonResult(result), nil, nil
		case "delete":
			result, err := ops.EnvDelete(ctx, client, projectID, input.ServiceHostname, input.Project, input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
			if result.Process != nil {
				result.Process, _ = pollManageProcess(ctx, client, result.Process, onProgress)
			}
			result.NextActions = nextActionEnvDeleteSuccess
			return jsonResult(result), nil, nil
		case "":
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use set or delete")), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use set or delete")), nil, nil
		}
	})
}
