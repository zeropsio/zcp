package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// KnowledgeInput is the input type for zerops_knowledge.
// Supports four modes: query (text search), briefing (contextual assembly), scope (platform reference), or recipe.
//
// v8.96 quality fix — every parameter description leads with `MODE N:`
// so the agent can pattern-match the mode taxonomy from any one field
// it consults. Combined with the tool-level Description rewrite below,
// this eliminates the v31 "5 retries" pattern where the agent kept
// passing two-mode combos (query + recipe, runtime + scope, etc.) and
// learning the rules by trial-and-error from rejection messages.
type KnowledgeInput struct {
	Query    string   `json:"query,omitempty"    jsonschema:"MODE 1 (query — free-text search). Pass a topic phrase like 'readiness check', 'cross-service wiring'. ONLY for unknown topics — for any named guide ({runtime}-hello-world, {framework}-minimal, {framework}-{ssr,static}-hello-world), use the recipe= field instead. Use alone — combining with runtime/services/scope/recipe is rejected."`
	Limit    int      `json:"limit,omitempty"    jsonschema:"MODE 1 helper — maximum number of search results (query mode only). Has no effect in other modes."`
	Runtime  string   `json:"runtime,omitempty"  jsonschema:"MODE 2 (briefing — stack-specific rules). Pass a runtime type with version, e.g. php-nginx@8.4 or bun@1.2. Combine with services= for full-stack briefings; use either field alone is also valid. Do NOT combine with query/scope/recipe."`
	Services []string `json:"services,omitempty" jsonschema:"MODE 2 (briefing — stack-specific rules). Pass service types with versions, e.g. [postgresql@16, valkey@7.2]. Combine with runtime= for full-stack briefings; use either field alone is also valid. Do NOT combine with query/scope/recipe."`
	Recipe   string   `json:"recipe,omitempty"   jsonschema:"MODE 4 (recipe — consume an existing published guide). Valid shapes: {runtime}-hello-world, {framework}-{ssr,static}-hello-world, {framework}-minimal. For named lookups of already-published guides, use this field instead of query=. Do NOT use while authoring a new recipe via zerops_recipe — authoring has its own research pipeline. Use alone — combining with query/runtime/services/scope is rejected."`
	Scope    string   `json:"scope,omitempty"    jsonschema:"MODE 3 (scope — full platform reference). Only valid value is 'infrastructure' — returns complete Zerops knowledge (YAML schemas, env vars, build/deploy lifecycle). Required before generating YAML in develop/bootstrap workflows. Do NOT call during zerops_recipe authoring — that pipeline emits YAML deterministically from typed plan state. Use alone — combining with query/runtime/services/recipe is rejected."`
	Mode     string   `json:"mode,omitempty"     jsonschema:"OPTIONAL helper, ANY mode — override the auto-detected workflow mode filter (dev, standard, simple, stage). Auto-detected from the active workflow session when omitted. Common use: mode=stage during a dev/standard workflow to see prod deploy patterns. Does NOT count as a mode-selecting field."`
}

// describeKnowledgeModes renders the modes the caller passed as a
// readable list for use in rejection messages. Order matches the
// canonical decision tree in the tool description (recipe → scope →
// briefing → query) so the agent's mental map of mode taxonomy stays
// consistent across passes.
func describeKnowledgeModes(hasRecipe, hasScope, hasBriefing, hasQuery bool) string {
	var parts []string
	if hasRecipe {
		parts = append(parts, "MODE 4 recipe=")
	}
	if hasScope {
		parts = append(parts, "MODE 3 scope=")
	}
	if hasBriefing {
		parts = append(parts, "MODE 2 runtime=/services=")
	}
	if hasQuery {
		parts = append(parts, "MODE 1 query=")
	}
	return strings.Join(parts, " + ")
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
		return string(state.Bootstrap.PlanMode())
	}
	return ""
}

// RegisterKnowledge registers the zerops_knowledge tool.
func RegisterKnowledge(srv *mcp.Server, store knowledge.Provider, client platform.Client, cache *ops.StackTypeCache, tracker *ops.KnowledgeTracker, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_knowledge",
		Description: "Read-only Zerops knowledge. NOT during zerops_recipe research phase — services/versions come from the research atom. After research, scaffold/feature/writer sub-agents SHOULD consult for managed-service connection patterns (postgresql, valkey, nats, object-storage, meilisearch) before writing client code. Pick ONE mode (mixing rejected): recipe=NAME reads a guide; scope=\"infrastructure\" before YAML in develop/bootstrap; runtime=/services= for stack briefing; query=\"phrase\" for free-text search.",
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
				"No mode selected — zerops_knowledge requires exactly one of: recipe, scope, runtime/services, or query",
				"Pick the mode that matches your intent: recipe=\"NAME\" for named guide, scope=\"infrastructure\" before writing YAML, runtime=/services= for stack briefing, query=\"phrase\" for free-text search.")), nil, nil
		}

		if modeCount > 1 {
			// Name the specific combination the agent passed so the
			// rejection points at the conflict instead of restating the
			// rule generically. The agent can fix the call from the
			// "what was passed" list alone.
			combo := describeKnowledgeModes(hasRecipe, hasScope, hasBriefing, hasQuery)
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Multiple modes selected (%s) — pick exactly one", combo),
				"Re-call with only one of: recipe=\"NAME\" alone, OR scope=\"infrastructure\" alone, OR runtime=/services= alone, OR query=\"phrase\" alone. The mode= field is the only one that combines with any of these.")), nil, nil
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
