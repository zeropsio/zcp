package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

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
		// includeProjectEnvs=true: the discover tool always surfaces project
		// envs alongside scoped service envs so agents can read launch /
		// debug / env-wiring context in one call. The ops layer defaults
		// this false so zerops_env action="get" (which delegates to Discover)
		// keeps its narrow service-only contract — see env.go.
		result, err := ops.Discover(ctx, client, projectID, input.Service, input.IncludeEnvs.Bool(), input.IncludeEnvValues.Bool(), true)
		if err != nil {
			return convertError(err), nil, nil
		}
		enrichWithMetaStatus(result, stateDir)
		return jsonResult(result), nil, nil
	})
}

// enrichWithMetaStatus sets ManagedByZCP on each service based on
// ServiceMeta presence, detects SSHFS mount paths for services mounted
// locally, and — when the project has live runtime services that ZCP
// doesn't yet track — appends a directive warning to result.Warnings.
//
// The warning replaces an earlier structured `adoptRecovery` field
// (v9.101.2 .. v9.101.3) that agents demonstrably skimmed past: the
// "Recovery" field name mapped to the "passive fallback" mental
// bucket and got ignored in favor of in-flight intent inference
// (t3.txt agent introspection: "I read it but classified as advisory,
// not precondition"). Warnings array agents parse as a prominent
// system-message bucket — same surface ZCP already uses for other
// project-level alerts. Combined with the existing
// workflow=develop ADOPT_REQUIRED hard gate (already follow-throughs
// reliably), the warning closes the discovery → adopt → develop
// detour cost-free on the first call.
func enrichWithMetaStatus(result *ops.DiscoverResult, stateDir string) {
	// Detect mounts regardless of stateDir.
	for i := range result.Services {
		path := "/var/www/" + result.Services[i].Hostname
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			result.Services[i].MountPath = path
		}
	}

	if stateDir != "" {
		metas, err := workflow.ListServiceMetas(stateDir)
		if err == nil && len(metas) > 0 {
			// Pair-keyed index: both halves of a standard-mode pair resolve
			// to the shared meta (spec-workflows.md §8 E8). Layer an
			// IsComplete filter on top because ManagedByZCP should reflect
			// a fully-bootstrapped state.
			idx := workflow.ManagedRuntimeIndex(metas)
			for i := range result.Services {
				if m, ok := idx[result.Services[i].Hostname]; ok && m.IsComplete() {
					result.Services[i].ManagedByZCP = true
				}
			}
		}
	}

	// Enumerate non-system runtime services that have NO IsComplete
	// meta. Managed services (db / cache / storage — IsInfrastructure)
	// are not adopt candidates; they live as API-authoritative
	// dependencies of a runtime adoption. The ZCP control-plane
	// container (type=zcp@1) is also excluded — it's the host running
	// THIS process, never a promotion target. Mirror of
	// launch_source_context.go::isZCPSelfService gating.
	var unmanaged []string
	for i := range result.Services {
		s := &result.Services[i]
		if s.IsInfrastructure {
			continue
		}
		if s.ManagedByZCP {
			continue
		}
		if s.Type == "zcp@1" {
			continue
		}
		unmanaged = append(unmanaged, s.Hostname)
	}
	if len(unmanaged) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"Services not adopted by ZCP: %s. Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"` to adopt them BEFORE any service-scoped work (develop, deploy, verify) — those calls will reject with ADOPT_REQUIRED until adoption completes.",
			strings.Join(unmanaged, ", "),
		))
	}
}
