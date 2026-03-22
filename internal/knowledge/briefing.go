package knowledge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// GetBriefing assembles stack-specific knowledge using layered composition.
// Layers: live stacks -> runtime delta -> recipes -> service cards -> wiring -> decisions -> version check.
// Core reference (platform model, YAML schemas) is NOT included — use GetCore() via scope="infrastructure".
// runtime: e.g. "php-nginx@8.4" (normalized internally to "PHP" section)
// services: e.g. ["postgresql@16", "valkey@7.2"] (normalized to section names)
// liveTypes: optional live service stack types for version validation and stack listing (nil = skip)
// Returns assembled markdown content ready for LLM consumption.
func (s *Store) GetBriefing(runtime string, services []string, mode string, liveTypes []platform.ServiceStackType) (string, error) {
	// Auto-promote: if runtime is empty but a known runtime name is in services, promote it.
	// This handles the common agent mistake of passing runtimes in the services array.
	if runtime == "" && len(services) > 0 {
		runtime, services = autoPromoteRuntime(services)
	}

	var sb strings.Builder

	// Live service stacks (if available)
	if stacks := FormatServiceStacks(liveTypes); stacks != "" {
		sb.WriteString(stacks)
		sb.WriteString("\n\n")
	}

	// L3: Runtime guide (specific runtime only)
	runtimeBase := ""
	if runtime != "" {
		runtimeBase, _, _ = strings.Cut(runtime, "@")
		slug := normalizeRuntimeName(runtime)
		if slug != "" {
			if guide := s.getRuntimeGuide(slug); guide != "" {
				if mode != "" {
					guide = filterDeployPatterns(guide, mode)
				}
				sb.WriteString(guide)
				sb.WriteString("\n\n---\n\n")
			}
		}
	}

	// L3b: Matching recipes hint
	if runtimeBase != "" {
		if recipes := s.matchingRecipes(runtimeBase); len(recipes) > 0 {
			sb.WriteString("## Matching Recipes\n\n")
			sb.WriteString("**If you are using any of these frameworks, load the recipe NOW** — it contains required secrets, scaffolding, and gotchas:\n")
			for _, r := range recipes {
				sb.WriteString("- `")
				sb.WriteString(r)
				sb.WriteString("`\n")
			}
			sb.WriteString("\n---\n\n")
		}
	}

	// L4: Service cards (per service, only relevant ones)
	if len(services) > 0 {
		sb.WriteString("## Service Cards\n\n")
		for _, svc := range services {
			normalized := normalizeServiceName(svc)
			if card := s.getServiceCard(normalized); card != "" {
				sb.WriteString("### ")
				sb.WriteString(normalized)
				sb.WriteString("\n\n")
				sb.WriteString(card)
				sb.WriteString("\n\n")
			}
		}
		sb.WriteString("---\n\n")
	}

	// L5: Wiring syntax rules (per-service wiring is already in service cards)
	if len(services) > 0 {
		if syntax := s.getWiringSyntax(); syntax != "" {
			sb.WriteString("## Wiring Patterns\n\n")
			sb.WriteString(syntax)
			sb.WriteString("\n\n")
		}
	}

	// L6: Relevant decisions (compact hints based on stack)
	if decisions := s.getRelevantDecisions(runtime, services); decisions != "" {
		sb.WriteString("## Decision Hints\n\n")
		sb.WriteString(decisions)
		sb.WriteString("\n\n")
	}

	// L7: Version check (if live types available)
	if versionCheck := FormatVersionCheck(runtime, services, liveTypes); versionCheck != "" {
		sb.WriteString("---\n\n")
		sb.WriteString(versionCheck)
	}

	return sb.String(), nil
}

// GetRecipe returns the full content of a named recipe, prepended with platform universals
// and an auto-detected runtime guide.
// name: recipe filename without extension (e.g., "laravel")
// Resolution chain: exact match → single fuzzy → disambiguation list → error.
func (s *Store) GetRecipe(name, mode string) (string, error) {
	// Try exact match first.
	uri := "zerops://recipes/" + name
	if doc, err := s.Get(uri); err == nil {
		content := s.prependRecipeContext(name, doc.Content)
		if mode != "" {
			rt := s.detectRecipeRuntime(name)
			content = prependModeAdaptation(mode, rt) + content
		}
		return content, nil
	}

	// Fuzzy fallback: find matching recipes.
	matches := s.findMatchingRecipes(name)
	switch len(matches) {
	case 0:
		available := s.ListRecipes()
		return "", fmt.Errorf("recipe %q not found (available: %s)", name, strings.Join(available, ", "))
	case 1:
		// Auto-resolve single match.
		doc, err := s.Get("zerops://recipes/" + matches[0])
		if err != nil {
			return "", fmt.Errorf("recipe %q not found: %w", matches[0], err)
		}
		content := s.prependRecipeContext(matches[0], doc.Content)
		if mode != "" {
			rt := s.detectRecipeRuntime(matches[0])
			content = prependModeAdaptation(mode, rt) + content
		}
		return content, nil
	default:
		// Multiple matches — return disambiguation.
		return s.formatDisambiguation(name, matches), nil
	}
}

// detectRecipeRuntime uses reverse runtimeRecipeHints to find the primary runtime for a recipe.
// Skips "static" hits because merged recipes appear in both nodejs and static hints;
// the Node.js runtime guide is the richer context.
func (s *Store) detectRecipeRuntime(recipeName string) string {
	for runtimeBase, prefixes := range runtimeRecipeHints {
		for _, prefix := range prefixes {
			if strings.HasPrefix(recipeName, prefix) {
				if runtimeBase == runtimeStatic {
					continue
				}
				return runtimeBase
			}
		}
	}
	return ""
}

// prependRecipeContext prepends universals and an auto-detected runtime guide to recipe content.
func (s *Store) prependRecipeContext(recipeName, content string) string {
	if rt := s.detectRecipeRuntime(recipeName); rt != "" {
		if guide := s.getRuntimeGuide(rt); guide != "" {
			content = guide + "\n\n---\n\n" + content
		}
	}
	return s.prependUniversals(content)
}

// findMatchingRecipes returns recipe names matching the query via prefix, substring, or keyword.
// Case-insensitive. Results are deduplicated and sorted alphabetically.
func (s *Store) findMatchingRecipes(query string) []string {
	queryLower := strings.ToLower(query)
	allRecipes := s.ListRecipes()
	seen := make(map[string]bool, len(allRecipes))

	for _, name := range allRecipes {
		nameLower := strings.ToLower(name)
		// Prefix match.
		if strings.HasPrefix(nameLower, queryLower) {
			seen[name] = true
			continue
		}
		// Substring match.
		if strings.Contains(nameLower, queryLower) {
			seen[name] = true
			continue
		}
		// Keyword match.
		doc, err := s.Get("zerops://recipes/" + name)
		if err != nil {
			continue
		}
		for _, kw := range doc.Keywords {
			if strings.EqualFold(kw, queryLower) {
				seen[name] = true
				break
			}
		}
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// formatDisambiguation returns a disambiguation message listing matching recipes with TL;DR.
func (s *Store) formatDisambiguation(query string, matches []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Multiple recipes match %q. Specify the full name:\n\n", query))
	for _, name := range matches {
		sb.WriteString("- **")
		sb.WriteString(name)
		sb.WriteString("**")
		if doc, err := s.Get("zerops://recipes/" + name); err == nil && doc.TLDR != "" {
			sb.WriteString(" — ")
			sb.WriteString(doc.TLDR)
		}
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("\nUse: zerops_knowledge recipe=\"%s\"", matches[0]))
	return sb.String()
}

// prependUniversals prepends platform universals to content, falling back to content alone.
func (s *Store) prependUniversals(content string) string {
	universals, err := s.GetUniversals()
	if err != nil {
		return content
	}
	return universals + "\n\n---\n\n" + content
}

// filterDeployPatterns filters the "### Deploy Patterns" section of a runtime guide
// to show only the pattern relevant to the given mode.
// mode mapping: "dev"/"standard" → keep **Dev deploy**, "simple" → keep **Dev deploy**,
// "stage" → keep **Prod deploy**. Empty mode returns the guide unchanged.
func filterDeployPatterns(guide, mode string) string {
	const header = "### Deploy Patterns"
	idx := strings.Index(guide, header)
	if idx < 0 {
		return guide
	}

	// Find the end of the Deploy Patterns section (next ### or end of string).
	sectionStart := idx + len(header)
	rest := guide[sectionStart:]
	sectionEnd := strings.Index(rest, "\n### ")
	var section string
	if sectionEnd < 0 {
		section = rest
		sectionEnd = len(rest)
	} else {
		section = rest[:sectionEnd]
	}

	var keepPrefix string
	switch mode {
	case "dev", "standard":
		keepPrefix = "**Dev deploy**:"
	case "simple":
		// Simple is hybrid: deployFiles from dev + start/healthCheck from prod.
		// Show both patterns so the agent has full context.
		return guide
	case "stage":
		keepPrefix = "**Prod deploy**:"
	default:
		return guide
	}

	// Filter lines within the section.
	var filtered []string
	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		// Keep empty lines and lines matching our mode prefix.
		if trimmed == "" || strings.HasPrefix(trimmed, keepPrefix) {
			filtered = append(filtered, line)
		}
		// Drop lines starting with other deploy pattern prefixes.
	}

	return guide[:idx] + header + strings.Join(filtered, "\n") + rest[sectionEnd:]
}

// isImplicitWebserverRuntime returns true for runtimes with built-in web servers
// that need no start command or explicit port configuration.
// Accepts base names as returned by detectRecipeRuntime (e.g., "php")
// and full runtime prefixes (e.g., "php-nginx").
func isImplicitWebserverRuntime(runtimeBase string) bool {
	switch runtimeBase {
	case "php", "php-nginx", "php-apache", runtimeNginx, runtimeStatic:
		return true
	}
	return false
}

// prependModeAdaptation returns a mode-specific adaptation header for recipes.
// runtime is the base runtime name (e.g., "php", "nodejs") from detectRecipeRuntime.
// Empty runtime is treated as dynamic (safe default).
func prependModeAdaptation(mode, runtime string) string {
	implicit := isImplicitWebserverRuntime(runtime)
	switch mode {
	case "dev", "standard":
		var startNote string
		if implicit {
			startNote = "> - Omit `start:` and `run.ports` entirely (webserver is built-in)\n"
		} else {
			startNote = "> - Use `start: zsc noop --silent` (not the production start command)\n"
		}
		return "> **Mode: dev** — This recipe shows production patterns. For your dev entry:\n" +
			"> - Use `deployFiles: [.]` (not the production pattern below)\n" +
			startNote +
			"> - Omit `healthCheck` (you control the server manually)\n" +
			"> The build commands and dependencies from this recipe still apply.\n\n"
	case "simple":
		if implicit {
			return "> **Mode: simple** — Use the production patterns below but keep `deployFiles: [.]`\n" +
				"> since this is a self-deploying service. Omit `start:` and `run.ports` (webserver built-in).\n" +
				"> `healthCheck` applies as-is.\n\n"
		}
		return "> **Mode: simple** — Use the production patterns below but keep `deployFiles: [.]`\n" +
			"> since this is a self-deploying service. The start command and healthCheck apply as-is.\n\n"
	default:
		return ""
	}
}

// ListRecipes returns names of all available recipes (without extension).
func (s *Store) ListRecipes() []string {
	var recipes []string
	prefix := "zerops://recipes/"
	for uri := range s.docs {
		if name, ok := strings.CutPrefix(uri, prefix); ok {
			recipes = append(recipes, name)
		}
	}
	sort.Strings(recipes)
	return recipes
}
