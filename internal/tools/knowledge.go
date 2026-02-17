package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// KnowledgeInput is the input type for zerops_knowledge.
// Supports four modes: query (BM25 search), briefing (contextual assembly), scope (platform reference), or recipe.
type KnowledgeInput struct {
	Query    string   `json:"query,omitempty"`    // BM25 search query
	Limit    int      `json:"limit,omitempty"`    // Result limit for query mode
	Runtime  string   `json:"runtime,omitempty"`  // Runtime type (e.g., "php-nginx@8.4")
	Services []string `json:"services,omitempty"` // Service types (e.g., ["postgresql@16", "valkey@7.2"])
	Recipe   string   `json:"recipe,omitempty"`   // Recipe name (e.g., "laravel-jetstream")
	Scope    string   `json:"scope,omitempty"`    // "infrastructure" for platform reference
}

// RegisterKnowledge registers the zerops_knowledge tool.
func RegisterKnowledge(srv *mcp.Server, store knowledge.Provider, client platform.Client, cache *ops.StackTypeCache) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_knowledge",
		Description: "Load Zerops platform knowledge. Four modes: (1) briefing — stack-specific rules. Use `runtime` and/or `services` params. Returns: binding rules, ports, env vars, wiring patterns, version validation. (2) scope — platform reference. Use scope=\"infrastructure\" for complete Zerops knowledge: import.yml/zerops.yml schemas, env var system (cross-service references, envSecrets), build/deploy lifecycle, rules & pitfalls. Required before generating YAML. (3) query — BM25 search for specific topics. (4) recipe — pre-built configs for frameworks.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Zerops knowledge access",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input KnowledgeInput) (*mcp.CallToolResult, any, error) {
		// Validate: at least one mode specified
		hasQuery := input.Query != ""
		hasBriefing := input.Runtime != "" || len(input.Services) > 0
		hasRecipe := input.Recipe != ""
		hasScope := input.Scope != ""

		modeCount := 0
		if hasQuery {
			modeCount++
		}
		if hasBriefing {
			modeCount++
		}
		if hasRecipe {
			modeCount++
		}
		if hasScope {
			modeCount++
		}

		if modeCount == 0 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Must provide at least one of: query, runtime/services, scope, or recipe",
				"Specify query for BM25 search, runtime/services for briefing, scope for platform reference, or recipe for a recipe")), nil, nil
		}

		if modeCount > 1 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Cannot mix query, briefing, scope, and recipe modes",
				"Use only one mode per call")), nil, nil
		}

		// Mode 1: Scope (platform reference)
		if hasScope {
			if input.Scope != "infrastructure" {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Unknown scope %q", input.Scope),
					"Use scope=\"infrastructure\" for platform reference")), nil, nil
			}
			core, err := store.GetCore()
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrFileNotFound,
					fmt.Sprintf("Failed to load core reference: %v", err),
					"Check that core knowledge files are embedded")), nil, nil
			}
			return textResult(core), nil, nil
		}

		// Mode 2: Search
		if hasQuery {
			results := store.Search(input.Query, input.Limit)
			return jsonResult(results), nil, nil
		}

		// Mode 3: Contextual briefing
		if hasBriefing {
			var liveTypes []platform.ServiceStackType
			if client != nil && cache != nil {
				liveTypes = cache.Get(ctx, client)
			}
			briefing, err := store.GetBriefing(input.Runtime, input.Services, liveTypes)
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrFileNotFound,
					fmt.Sprintf("Failed to assemble briefing: %v", err),
					"Check that core knowledge files are embedded")), nil, nil
			}
			return textResult(briefing), nil, nil
		}

		// Mode 4: Recipe retrieval
		if hasRecipe {
			recipe, err := store.GetRecipe(input.Recipe)
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					err.Error(),
					"Check recipe name spelling and available recipes")), nil, nil
			}
			return textResult(recipe), nil, nil
		}

		// Should never reach here
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidUsage, "Invalid mode routing", "")), nil, nil
	})
}
