package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EnvInput is the input type for zerops_env.
type EnvInput struct {
	Action          string   `json:"action"                    jsonschema:"Action: set, delete, or generate-dotenv. generate-dotenv reads zerops.yaml envVariables, resolves ${hostname_varName} refs via API, and writes .env file."`
	ServiceHostname string   `json:"serviceHostname,omitempty" jsonschema:"Hostname of the service to modify env vars on. Required unless project=true."`
	Project         bool     `json:"project,omitempty"         jsonschema:"Set to true to manage project-level env vars instead of service-level."`
	Variables       []string `json:"variables,omitempty"       jsonschema:"List of env vars. For set: KEY=VALUE strings. For delete: KEY names only."`
}

// RegisterEnv registers the zerops_env tool.
func RegisterEnv(srv *mcp.Server, client platform.Client, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_env",
		Description: "Manage environment variables. Actions: set, delete, generate-dotenv. Scope: service (serviceHostname) or project (project=true). After set/delete, RESTART affected services — env vars only take effect after restart. For project-level vars, restart ALL running services. generate-dotenv: resolves ${hostname_varName} refs, writes .env file. To read keys, use zerops_discover includeEnvs=true.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Manage environment variables",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EnvInput) (*mcp.CallToolResult, any, error) {
		onProgress := buildProgressCallback(ctx, req)

		switch input.Action {
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
		case "generate-dotenv":
			result, err := ops.EnvGenerateDotenv(ctx, client, projectID, input.ServiceHostname, "")
			if err != nil {
				return convertError(err), nil, nil
			}
			return jsonResult(result), nil, nil
		case "":
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Action is required",
				"Use set, delete, or generate-dotenv")), nil, nil
		default:
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter, "Invalid action '"+input.Action+"'",
				"Use set, delete, or generate-dotenv")), nil, nil
		}
	})
}
