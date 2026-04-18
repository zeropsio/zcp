package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// KnowledgeInput is the input type for zerops_knowledge.
// Supports four modes: query (text search), briefing (contextual assembly), scope (platform reference), or recipe.
type KnowledgeInput struct {
	Query    string   `json:"query,omitempty"    jsonschema:"Free-text topic search across Zerops docs (e.g. 'readiness check', 'cross-service wiring'). NOT for fetching known guides by name — if you have a recipe/hello-world name, use recipe= instead. Use alone (query mode)."`
	Limit    int      `json:"limit,omitempty"    jsonschema:"Maximum number of search results to return (query mode only)."`
	Runtime  string   `json:"runtime,omitempty"  jsonschema:"Runtime type for stack briefing (e.g. php-nginx@8.4 or bun@1.2). Use with or without services (briefing mode)."`
	Services []string `json:"services,omitempty" jsonschema:"Service types for stack briefing (e.g. [postgresql@16, valkey@7.2]). Use with or without runtime (briefing mode)."`
	Recipe   string   `json:"recipe,omitempty"   jsonschema:"Name of a pre-authored guide in the knowledge store. Valid shapes: {runtime}-hello-world (runtime primer — go, bun, php, python, nodejs, deno, dotnet, gleam, java, ruby, rust), {framework}-{ssr,static}-hello-world (frontend framework primer — nextjs-ssr, vue-static, svelte, ...), or {framework}-minimal (backend framework recipe — laravel-minimal, django-minimal, ...). ALWAYS use this field for any named guide lookup — never query= for a known name. Use alone (recipe mode)."`
	Scope    string   `json:"scope,omitempty"    jsonschema:"Platform reference scope. Use scope=infrastructure for complete Zerops knowledge (YAML schemas, env vars, build/deploy lifecycle). Required before generating YAML. Use alone (scope mode)."`
	Mode     string   `json:"mode,omitempty"     jsonschema:"Override mode filter (dev, standard, simple, stage). Auto-detected from active workflow session if omitted. Use mode=stage to see prod deploy patterns during dev/standard workflows."`
}

// resolveKnowledgeMode determines the mode filter for knowledge responses.
// Explicit inputMode takes priority. Otherwise auto-detects from active/completed session.
// Returns "" when no context is available (knowledge returned unfiltered).
func resolveKnowledgeMode(engine *workflow.Engine, inputMode string) string {
	if inputMode != "" {
		return inputMode
	}
	if engine == nil {
		return ""
	}
	state, err := engine.GetState()
	if err != nil {
		return ""
	}
	if state.Bootstrap != nil {
		return state.Bootstrap.PlanMode()
	}
	return ""
}

// RegisterKnowledge registers the zerops_knowledge tool.
func RegisterKnowledge(srv *mcp.Server, store knowledge.Provider, client platform.Client, cache *ops.StackTypeCache, tracker *ops.KnowledgeTracker, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_knowledge",
		Description: "Load Zerops platform knowledge. Four modes: (1) briefing — stack-specific rules via runtime/services params. (2) scope=infrastructure — complete platform reference, required before generating YAML. (3) query — free-text topic search (NOT for fetching known guides — use recipe= for those). (4) recipe — named guide from store: runtime primer ({runtime}-hello-world), frontend primer ({framework}-{ssr,static}-hello-world), or backend framework recipe ({framework}-minimal).",
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
				"Specify query for text search, runtime/services for briefing, scope for platform reference, or recipe for a recipe")), nil, nil
		}

		if modeCount > 1 {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Cannot mix query, briefing, scope, and recipe modes",
				"Use only one mode per call")), nil, nil
		}

		// Mode 1: Scope (platform reference) — always returns full content.
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
			result := core
			if model, mErr := store.GetModel(); mErr == nil {
				result = model + "\n\n---\n\n" + result
			}
			if client != nil && cache != nil {
				if types := cache.Get(ctx, client); len(types) > 0 {
					result = knowledge.FormatStackList(types) + "\n---\n\n" + result
				}
			}
			if tracker != nil {
				tracker.RecordScope()
			}
			return textResult(result), nil, nil
		}

		// Mode 2: Search — with session-level cache to avoid redundant
		// searches when Claude Code cancels parallel calls and retries.
		if hasQuery {
			cacheKey := fmt.Sprintf("query|%s|%d", input.Query, input.Limit)
			if cached, ok := engine.GetKnowledgeCache(cacheKey); ok {
				if r, ok := cached.(*mcp.CallToolResult); ok {
					return r, nil, nil
				}
			}
			results := store.Search(input.Query, input.Limit)
			result := jsonResult(results)
			engine.SetKnowledgeCache(cacheKey, result)
			return result, nil, nil
		}

		// Mode 3: Contextual briefing — filtered by session mode when available.
		if hasBriefing {
			var liveTypes []platform.ServiceStackType
			if client != nil && cache != nil {
				liveTypes = cache.Get(ctx, client)
			}
			mode := resolveKnowledgeMode(engine, input.Mode)
			briefing, err := store.GetBriefing(input.Runtime, input.Services, mode, liveTypes)
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrFileNotFound,
					fmt.Sprintf("Failed to assemble briefing: %v", err),
					"Check that core knowledge files are embedded")), nil, nil
			}
			if tracker != nil {
				tracker.RecordBriefing(input.Runtime, input.Services)
			}
			return textResult(briefing), nil, nil
		}

		// Mode 4: Recipe retrieval — mode-adapted when session context available.
		if hasRecipe {
			mode := resolveKnowledgeMode(engine, input.Mode)
			recipe, err := store.GetRecipe(input.Recipe, mode)
			if errors.Is(err, knowledge.ErrAmbiguousRecipe) {
				// Multiple fuzzy matches: the "recipe" text is the disambiguation
				// list — still agent-friendly, just not auto-resolved.
				return textResult(recipe), nil, nil
			}
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
