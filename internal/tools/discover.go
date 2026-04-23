package tools

import (
	"context"
	"os"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DiscoverInput is the input type for zerops_discover.
//
// IncludeEnvs and IncludeEnvValues are FlexBool so the MCP schema
// accepts both JSON booleans and their stringified forms — some LLM
// agents serialize tool arguments with quoted primitives and the
// earlier bool-only schema rejected them with a non-actionable error
// (seen in the v7 post-mortem log, attempt 1 of discover failed with
// `includeEnvs has type "string", want "boolean"`). The explicit
// InputSchema below publishes a matching `oneOf: [boolean, string]`
// so the protocol-level validator agrees.
type DiscoverInput struct {
	Service          string   `json:"service,omitempty"`
	IncludeEnvs      FlexBool `json:"includeEnvs,omitempty"`
	IncludeEnvValues FlexBool `json:"includeEnvValues,omitempty"`
}

// discoverInputSchema is the explicit InputSchema for zerops_discover.
// Hand-written so we can mark the two env toggles as `oneOf: [boolean,
// string-enum]` — the reflection-based schema would have rejected the
// stringified form used by some agents. Field descriptions live here
// rather than on struct tags so the two are co-located with the schema
// types they describe.
func discoverInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"service": {
			Type:        "string",
			Description: "Filter by service hostname. Omit to list all services in the project. When discovering env vars for multiple services, omit this parameter — one call returns all.",
		},
		"includeEnvs":      flexBoolSchema("Include env var keys (service-level and project-level). Returns keys and annotations only — no values. Sufficient for bootstrap, deploy, recipe validation."),
		"includeEnvValues": flexBoolSchema("Also include actual env var values. Use only for troubleshooting when keys-only is insufficient (e.g. empty values, wrong formats, unresolved refs). For .env generation use zerops_env generate-dotenv instead."),
	})
}

// RegisterDiscover registers the zerops_discover tool.
func RegisterDiscover(srv *mcp.Server, client platform.Client, projectID, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_discover",
		Description: "Discover project and service information. Filter by service hostname or list all. Use includeEnvs=true to read env var keys. Add includeEnvValues=true only when you need actual secret values (troubleshooting).",
		InputSchema: discoverInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Discover project and services",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DiscoverInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.Discover(ctx, client, projectID, input.Service, input.IncludeEnvs.Bool(), input.IncludeEnvValues.Bool())
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
	// Pair-keyed index: both halves of a standard-mode pair resolve to the
	// shared meta (spec-workflows.md §8 E8). Layer an IsComplete filter on
	// top because ManagedByZCP should reflect a fully-bootstrapped state.
	idx := workflow.ManagedRuntimeIndex(metas)
	for i := range result.Services {
		if m, ok := idx[result.Services[i].Hostname]; ok && m.IsComplete() {
			result.Services[i].ManagedByZCP = true
		}
	}
}
