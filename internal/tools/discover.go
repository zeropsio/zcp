package tools

import (
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DiscoverInput is the input type for zerops_discover.
type DiscoverInput struct {
	Service     string `json:"service,omitempty"     jsonschema:"Filter by service hostname. Omit to list all services in the project. When discovering env vars for multiple services, omit this parameter — one call returns all."`
	IncludeEnvs bool   `json:"includeEnvs,omitempty" jsonschema:"Include environment variables (both service-level and project-level) in the response. This is the primary way to read env vars. Without service filter, returns env vars for ALL services in one call."`
}

// RegisterDiscover registers the zerops_discover tool.
func RegisterDiscover(srv *mcp.Server, client platform.Client, projectID, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_discover",
		Description: "Discover project and service information. Filter by service hostname or list all. Use includeEnvs=true to read env vars — one call returns all services.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Discover project and services",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DiscoverInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Discover(ctx, client, projectID, input.Service, input.IncludeEnvs)
		if err != nil {
			return convertError(err), nil, nil
		}
		enrichWithMetaStatus(result, stateDir)
		return jsonResult(result), nil, nil
	})
}

// enrichWithMetaStatus sets ManagedByZCP on each service based on ServiceMeta presence,
// and detects SSHFS mount paths for services mounted locally.
func enrichWithMetaStatus(result *ops.DiscoverResult, stateDir string) {
	// Detect mounts regardless of stateDir.
	for i := range result.Services {
		path := "/var/www/" + result.Services[i].Hostname
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			result.Services[i].MountPath = path
		}
	}

	if stateDir == "" {
		return
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return
	}
	metaMap := make(map[string]bool, len(metas))
	for _, m := range metas {
		if m.IsComplete() {
			metaMap[m.Hostname] = true
		}
	}
	for i := range result.Services {
		if metaMap[result.Services[i].Hostname] {
			result.Services[i].ManagedByZCP = true
		}
	}
}
