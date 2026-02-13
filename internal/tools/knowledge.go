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
// Supports three modes: query (BM25 search), briefing (contextual assembly), or recipe.
type KnowledgeInput struct {
	Query    string   `json:"query,omitempty"`    // BM25 search query
	Limit    int      `json:"limit,omitempty"`    // Result limit for query mode
	Runtime  string   `json:"runtime,omitempty"`  // Runtime type (e.g., "php-nginx@8.4")
	Services []string `json:"services,omitempty"` // Service types (e.g., ["postgresql@16", "valkey@7.2"])
	Recipe   string   `json:"recipe,omitempty"`   // Recipe name (e.g., "laravel-jetstream")
}

// RegisterKnowledge registers the zerops_knowledge tool.
func RegisterKnowledge(srv *mcp.Server, store knowledge.Provider, client platform.Client, cache *ops.StackTypeCache) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_knowledge",
		Description: "Load Zerops platform rules and configuration knowledge. Three modes: (1) runtime/services — contextual briefing with version validation for YAML generation, (2) query — BM25 search for specific topics, (3) recipe — pre-built configs for frameworks. Call before generating any YAML.",
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

		if modeCount == 0 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Must provide at least one of: query, runtime/services, or recipe",
				"Specify query for BM25 search, runtime/services for briefing, or recipe for a recipe")), nil, nil
		}

		if modeCount > 1 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Cannot mix query, briefing, and recipe modes",
				"Use only one mode per call")), nil, nil
		}

		// Mode 1: BM25 search (existing behavior)
		if hasQuery {
			results := store.Search(input.Query, input.Limit)
			return jsonResult(results), nil, nil
		}

		// Mode 2: Contextual briefing (NEW)
		if hasBriefing {
			// Need concrete *Store for GetBriefing (not Provider interface)
			concreteStore, ok := store.(*knowledge.Store)
			if !ok {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidUsage,
					"Briefing mode requires concrete Store implementation",
					"Check server initialization")), nil, nil
			}

			var liveTypes []platform.ServiceStackType
			if client != nil && cache != nil {
				liveTypes = cache.Get(ctx, client)
			}
			briefing, err := concreteStore.GetBriefing(input.Runtime, input.Services, liveTypes)
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrFileNotFound,
					fmt.Sprintf("Failed to assemble briefing: %v", err),
					"Check that core knowledge files are embedded")), nil, nil
			}
			return textResult(briefing), nil, nil
		}

		// Mode 3: Recipe retrieval (NEW)
		if hasRecipe {
			// Need concrete *Store for GetRecipe
			concreteStore, ok := store.(*knowledge.Store)
			if !ok {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidUsage,
					"Recipe mode requires concrete Store implementation",
					"Check server initialization")), nil, nil
			}

			recipe, err := concreteStore.GetRecipe(input.Recipe)
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
