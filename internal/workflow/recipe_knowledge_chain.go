package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// recipeTierOrder defines tier precedence for knowledge chain resolution.
// Lower index = more basic. The chain injects knowledge from lower tiers.
var recipeTierOrder = []string{"hello-world", "minimal", "showcase"}

// recipeTierRank returns the rank of a recipe tier suffix (0=hello-world, 1=minimal, 2=showcase).
// Returns -1 for unknown tiers.
func recipeTierRank(slug string) int {
	for i, suffix := range recipeTierOrder {
		if strings.HasSuffix(slug, "-"+suffix) {
			return i
		}
	}
	return -1
}

// recipeKnowledgeChain finds and injects knowledge from lower-tier recipes.
//
// The chain is discovered by searching the recipe store — not hardcoded.
// For each recipe found with a lower tier rank than the current recipe:
//   - Direct predecessor (one tier below): full content
//   - Earlier ancestors (two+ tiers below): knowledge sections only (gotchas, base image)
//
// Search strategy: match by framework name first, then by runtime base.
// This finds e.g. "laravel-minimal" for framework="laravel" and "php-hello-world"
// for runtimeBase="php".
func recipeKnowledgeChain(plan *RecipePlan, kp knowledge.Provider) string {
	if plan == nil || kp == nil {
		return ""
	}

	currentRank := recipeTierRank(plan.Slug)
	if currentRank <= 0 {
		// Hello-world or unknown tier — nothing to inject.
		return ""
	}

	runtimeBase, _, _ := strings.Cut(plan.RuntimeType, "@")
	// Normalize runtime name for recipe matching (php-nginx → php, php-apache → php).
	runtimeBase = normalizeRuntimeBase(runtimeBase)

	// Discover related recipes from the store.
	allRecipes := kp.ListRecipes()
	candidates := findRelatedRecipes(allRecipes, plan.Framework, runtimeBase, plan.Slug)

	if len(candidates) == 0 {
		return ""
	}

	var parts []string

	for _, c := range candidates {
		tierDelta := currentRank - c.rank // 1 = direct predecessor, 2+ = earlier ancestor

		content, err := kp.GetRecipe(c.name, "")
		if err != nil || content == "" {
			continue
		}

		if tierDelta == 1 {
			// Direct predecessor: full content.
			header := fmt.Sprintf("## %s Recipe Knowledge (full)\n\nThis %s recipe was built for the same framework. Its zerops.yaml patterns, gotchas, and integration steps apply directly.\n\n",
				c.name, recipeTierOrder[c.rank])
			parts = append(parts, header+content)
		} else {
			// Earlier ancestor: knowledge sections only (gotchas, base image).
			knowledge := extractKnowledgeSections(content)
			if knowledge == "" {
				continue
			}
			header := fmt.Sprintf("## %s Platform Knowledge (gotchas only)\n\nPlatform-specific gotchas from the base runtime recipe. The zerops.yaml config is omitted — your recipe has its own.\n\n",
				c.name)
			parts = append(parts, header+knowledge)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// relatedRecipe holds a discovered recipe with its tier rank.
type relatedRecipe struct {
	name string
	rank int
}

// findRelatedRecipes searches the recipe store for recipes related to the
// current framework and runtime, excluding self, and returns them sorted
// by tier rank (lowest first = most basic first).
func findRelatedRecipes(allRecipes []string, framework, runtimeBase, currentSlug string) []relatedRecipe {
	currentRank := recipeTierRank(currentSlug)
	frameworkLower := strings.ToLower(framework)
	var candidates []relatedRecipe

	for _, name := range allRecipes {
		if name == currentSlug {
			continue
		}

		rank := recipeTierRank(name)
		if rank < 0 || rank >= currentRank {
			continue // Unknown tier or same/higher tier — only inject from LOWER tiers.
		}

		// Match by framework prefix (e.g., "laravel-minimal" matches framework "laravel").
		nameLower := strings.ToLower(name)
		if strings.HasPrefix(nameLower, frameworkLower+"-") {
			candidates = append(candidates, relatedRecipe{name: name, rank: rank})
			continue
		}

		// Match by runtime base (e.g., "php-hello-world" matches runtimeBase "php").
		if runtimeBase != "" && strings.HasPrefix(nameLower, runtimeBase+"-") {
			candidates = append(candidates, relatedRecipe{name: name, rank: rank})
		}
	}

	// Sort by rank ascending (most basic first).
	sortRelatedRecipes(candidates)
	return candidates
}

// sortRelatedRecipes sorts by rank ascending (stable).
func sortRelatedRecipes(candidates []relatedRecipe) {
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].rank < candidates[j-1].rank; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
}

// extractKnowledgeSections extracts knowledge content from a recipe — everything
// before the first numbered integration step (## 1. Adding `zerops.yaml`).
// This captures Gotchas, Base Image, and any other knowledge headers while
// stripping the zerops.yaml configuration and integration steps.
func extractKnowledgeSections(content string) string {
	lines := strings.Split(content, "\n")
	var out []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Stop at the first numbered section (integration steps).
		if strings.HasPrefix(trimmed, "## 1.") || strings.HasPrefix(trimmed, "## 1 ") {
			break
		}

		// Skip the recipe title (# Title) — the caller adds its own header.
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			continue
		}

		out = append(out, line)
	}

	result := strings.TrimSpace(strings.Join(out, "\n"))
	if result == "" {
		return ""
	}
	return result
}

// normalizeRuntimeBase maps composite runtime names to their base recipe prefix.
// e.g., "php-nginx" → "php", "php-apache" → "php".
// Simple runtimes pass through unchanged.
func normalizeRuntimeBase(base string) string {
	switch base {
	case "php-nginx", "php-apache":
		return "php"
	default:
		return base
	}
}
