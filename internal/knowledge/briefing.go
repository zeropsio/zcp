package knowledge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// GetBriefing assembles contextual knowledge for a specific stack using layered composition.
// Layers: L0 platform model -> L1 rules -> L2 grammar -> L3 runtime delta -> L3b recipes ->
// L4 service cards -> L5 wiring -> L6 decisions -> L7 version check.
// runtime: e.g. "php-nginx@8.4" (normalized internally to "PHP" section)
// services: e.g. ["postgresql@16", "valkey@7.2"] (normalized to section names)
// liveTypes: optional live service stack types for version validation (nil = skip)
// Returns assembled markdown content ready for LLM consumption.
func (s *Store) GetBriefing(runtime string, services []string, liveTypes []platform.ServiceStackType) (string, error) {
	var sb strings.Builder

	// L0: Platform model -- conceptual understanding (always included)
	if platformModel, err := s.GetPlatformModel(); err == nil {
		sb.WriteString(platformModel)
		sb.WriteString("\n\n")
	}

	// L1: Rules & pitfalls -- actionable DO/DON'T (always included)
	if rules, err := s.GetRules(); err == nil {
		sb.WriteString(rules)
		sb.WriteString("\n\n")
	}

	// L2: YAML schema reference (always included)
	grammar, err := s.GetFoundation()
	if err != nil {
		return "", err
	}
	sb.WriteString(grammar)
	sb.WriteString("\n\n")

	// L3: Runtime delta (specific runtime only)
	runtimeBase := ""
	if runtime != "" {
		runtimeBase, _, _ = strings.Cut(runtime, "@")
		normalized := normalizeRuntimeName(runtime)
		if normalized != "" {
			if section := s.getRuntimeException(normalized); section != "" {
				sb.WriteString("## Runtime-Specific: ")
				sb.WriteString(normalized)
				sb.WriteString("\n\n")
				sb.WriteString(section)
				sb.WriteString("\n\n---\n\n")
			}
		}
	}

	// L3b: Matching recipes hint
	if runtimeBase != "" {
		if recipes := s.matchingRecipes(runtimeBase); len(recipes) > 0 {
			sb.WriteString("## Matching Recipes\n\n")
			sb.WriteString("Available recipes for this runtime (use `zerops_knowledge recipe=\"name\"` to load):\n")
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

	// L5: Wiring (syntax rules + per-service templates)
	if len(services) > 0 {
		if syntax := s.getWiringSyntax(); syntax != "" {
			sb.WriteString("## Wiring Patterns\n\n")
			sb.WriteString(syntax)
			sb.WriteString("\n\n")
		}
		for _, svc := range services {
			normalized := normalizeServiceName(svc)
			if wiring := s.getWiringSection(normalized); wiring != "" {
				sb.WriteString("### Wiring: ")
				sb.WriteString(normalized)
				sb.WriteString("\n\n")
				sb.WriteString(wiring)
				sb.WriteString("\n\n")
			}
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

	sb.WriteString("\nNext: Generate import.yml and zerops.yml using the rules above. Use only validated versions. Then import with zerops_import.")

	return sb.String(), nil
}

// GetRecipe returns the full content of a named recipe.
// name: recipe filename without extension (e.g., "laravel-jetstream")
// Returns error if recipe not found.
func (s *Store) GetRecipe(name string) (string, error) {
	uri := "zerops://recipes/" + name
	doc, err := s.Get(uri)
	if err != nil {
		available := s.ListRecipes()
		return "", fmt.Errorf("recipe %q not found (available: %s)", name, strings.Join(available, ", "))
	}
	return doc.Content, nil
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
