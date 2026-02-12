package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EnvInput is the input type for zerops_env.
type EnvInput struct {
	Action          string   `json:"action"`
	ServiceHostname string   `json:"serviceHostname,omitempty"`
	Project         bool     `json:"project,omitempty"`
	Variables       []string `json:"variables,omitempty"`
}

// RegisterEnv registers the zerops_env tool.
func RegisterEnv(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_env",
		Description: "Manage environment variables. Actions: set, delete. Scope: service (serviceHostname) or project (project=true). To read env vars, use zerops_discover with includeEnvs=true.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage environment variables",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
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
			return jsonResult(result), nil, nil
		case "delete":
			result, err := ops.EnvDelete(ctx, client, projectID, input.ServiceHostname, input.Project, input.Variables)
			if err != nil {
				return convertError(err), nil, nil
			}
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
