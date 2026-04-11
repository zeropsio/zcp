package workflow

import (
	"fmt"
	"regexp"
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
			// Direct predecessor: Gotchas H2 + zerops.yaml template fence.
			// Integration-step prose (trust proxy, bind 0.0.0.0, env var
			// wiring) is dropped — it teaches existing-app integration, not
			// from-scratch generation. The YAML fence carries the framework
			// pattern; prose is noise for the generating agent.
			extracted := extractForPredecessor(content)
			if extracted == "" {
				continue
			}
			header := fmt.Sprintf("## %s Recipe Knowledge (predecessor)\n\nGotchas + zerops.yaml template from the direct predecessor recipe. Use the template as your starting point; adapt keys/services to your targets.\n\n",
				c.name)
			parts = append(parts, header+extracted)
		} else {
			// Earlier ancestor (tier delta ≥ 2): Gotchas only. Return empty
			// if the recipe has no ## Gotchas H2 — do NOT emit title-intro
			// filler as fake gotchas, which the old extractor did.
			extracted := extractForAncestor(content)
			if extracted == "" {
				continue
			}
			header := fmt.Sprintf("## %s Platform Knowledge (ancestor gotchas)\n\nPlatform-specific gotchas from a more basic recipe in the same runtime. zerops.yaml config is omitted — your recipe has its own.\n\n",
				c.name)
			parts = append(parts, header+extracted)
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

// extractForPredecessor returns content relevant to a tier-delta-1 injection:
// the Gotchas H2 plus the zerops.yaml code fence from "## 1. Adding
// zerops.yaml". Integration-step prose (trust proxy, bind 0.0.0.0, env var
// wiring) is dropped because it teaches existing-app integration, not
// from-scratch generation.
//
// Returns empty string when neither Gotchas nor a YAML template can be
// recovered. Callers must treat empty as "skip this predecessor entirely" —
// not as "emit the original content as fallback", which would re-introduce
// the integration prose noise.
func extractForPredecessor(content string) string {
	parts := []string{}
	if g := extractH2Section(content, "Gotchas"); g != "" {
		parts = append(parts, "## Gotchas\n\n"+g)
	}
	if tmpl := extractYAMLTemplate(content); tmpl != "" {
		parts = append(parts, "## zerops.yaml template (from predecessor recipe)\n\n"+tmpl)
	}
	return strings.Join(parts, "\n\n")
}

// extractForAncestor extracts only the Gotchas H2 from an ancestor recipe
// (tier delta ≥ 2). Returns empty string when no Gotchas section exists —
// the old "return everything before ## 1." rule emitted ~400 B of title-
// intro filler for every hello-world recipe in the store that lacked a
// ## Gotchas H2, which this function explicitly prevents.
func extractForAncestor(content string) string {
	g := extractH2Section(content, "Gotchas")
	if g == "" {
		return ""
	}
	return "## Gotchas (from ancestor recipe)\n\n" + g
}

// extractH2Section returns the body of the first H2 matching the given
// title (exact match after "## "). The walk stops at the next H2, the
// next H1, or EOF. Returns empty when no match is found.
func extractH2Section(content, title string) string {
	lines := strings.Split(content, "\n")
	inside := false
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			if inside {
				break
			}
			if strings.TrimPrefix(l, "## ") == title {
				inside = true
			}
			continue
		}
		if strings.HasPrefix(l, "# ") && !strings.HasPrefix(l, "## ") {
			if inside {
				break
			}
			continue
		}
		if inside {
			out = append(out, l)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// yamlFenceRe matches the first ```yaml or ```yml fenced code block.
// Reluctant `.*?` caps the match to the nearest closing fence.
var yamlFenceRe = regexp.MustCompile("(?s)```ya?ml\\s*\\n(.*?)\\n```")

// extractYAMLTemplate finds the first "## 1. Adding `zerops.yaml`" H2 and
// returns its first fenced yaml code block. Integration-step prose around
// the fence is dropped; only the template body (rewrapped in a fence)
// survives. Returns empty when neither the H2 nor a fence is found.
func extractYAMLTemplate(content string) string {
	lines := strings.Split(content, "\n")
	inside := false
	var h2Body strings.Builder
	for _, l := range lines {
		if strings.HasPrefix(l, "## 1.") || strings.HasPrefix(l, "## 1 ") {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(l, "## ") {
			break
		}
		if inside {
			h2Body.WriteString(l)
			h2Body.WriteByte('\n')
		}
	}
	if h2Body.Len() == 0 {
		return ""
	}
	m := yamlFenceRe.FindStringSubmatch(h2Body.String())
	if m == nil {
		return ""
	}
	return "```yaml\n" + m[1] + "\n```"
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
