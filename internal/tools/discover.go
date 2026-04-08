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
	Service          string `json:"service,omitempty"          jsonschema:"Filter by service hostname. Omit to list all services in the project. When discovering env vars for multiple services, omit this parameter — one call returns all."`
	IncludeEnvs      bool   `json:"includeEnvs,omitempty"      jsonschema:"Include env var keys (service-level and project-level). Returns keys and annotations only — no values. Sufficient for bootstrap, deploy, recipe validation."`
	IncludeEnvValues bool   `json:"includeEnvValues,omitempty" jsonschema:"Also include actual env var values. Use only for troubleshooting when keys-only is insufficient (e.g. empty values, wrong formats, unresolved refs). For .env generation use zerops_env generate-dotenv instead."`
}

// RegisterDiscover registers the zerops_discover tool.
func RegisterDiscover(srv *mcp.Server, client platform.Client, projectID, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_discover",
		Description: "Discover project and service information. Filter by service hostname or list all. Use includeEnvs=true to read env var keys. Add includeEnvValues=true only when you need actual secret values (troubleshooting).",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Discover project and services",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DiscoverInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Discover(ctx, client, projectID, input.Service, input.IncludeEnvs, input.IncludeEnvValues)
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
	metaMap := make(map[string]bool, len(metas)*2)
	for _, m := range metas {
		if m.IsComplete() {
			metaMap[m.Hostname] = true
			if m.StageHostname != "" {
				metaMap[m.StageHostname] = true
			}
		}
	}
	for i := range result.Services {
		if metaMap[result.Services[i].Hostname] {
			result.Services[i].ManagedByZCP = true
		}
	}
}
