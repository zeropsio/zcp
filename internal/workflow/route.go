package workflow

import (
	"context"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// BootstrapRoute is the bootstrap path chosen for a given intent + project state.
// The route is recorded once at bootstrap start and persists for the session.
type BootstrapRoute string

const (
	// BootstrapRouteRecipe — intent matches a viable recipe, infrastructure is empty.
	BootstrapRouteRecipe BootstrapRoute = "recipe"
	// BootstrapRouteClassic — no recipe match, fresh infra build.
	BootstrapRouteClassic BootstrapRoute = "classic"
	// BootstrapRouteAdopt — project already has non-managed services without meta.
	BootstrapRouteAdopt BootstrapRoute = "adopt"
)

// RecipeMatch describes a recipe that matched the user's intent above the
// minimum confidence threshold AND passed the viability gate. A RecipeMatch
// is only produced for viable matches — the gate filters out stubs.
//
// ImportYAML is the canonical project-import YAML that `zerops_import` should
// receive at the provision step. The recipe corpus fills it in at match time
// so the bootstrap conductor can inject it into the provision guide without
// a second store lookup (and so the match survives compaction).
type RecipeMatch struct {
	Slug       string  `json:"slug"`
	Confidence float64 `json:"confidence"`
	ImportYAML string  `json:"importYaml,omitempty"`
	// Mode is the bootstrap mode inferred from ImportYAML (standard, simple,
	// dev). Empty when the YAML shape is unrecognised or managed-only. Used
	// to reject plan submissions on the recipe route that deviate from the
	// recipe's intended mode.
	Mode string `json:"mode,omitempty"`
}

// RecipeCorpus abstracts the recipe search surface. Implementations live in
// internal/knowledge; the interface here keeps route selection testable
// without pulling the full knowledge engine into unit tests.
type RecipeCorpus interface {
	// FindViableMatch returns the best recipe match for intent that passes
	// the viability gate (DefaultRecipeViabilityRules). Returns nil when no
	// match clears the 0.85 confidence threshold OR no viable match exists.
	FindViableMatch(intent string) (*RecipeMatch, error)
}

// MinRecipeConfidence is the confidence threshold at which a recipe match is
// considered strong enough to take the happy path. Calibrated empirically in
// Phase 5 against a corpus of ≥20 labelled intents.
const MinRecipeConfidence = 0.85

// SelectBootstrapRoute picks one of three bootstrap paths per the strict order in
// §8.1. The first condition that matches wins:
//
//  1. Adopt when `existing` has any non-system, non-managed service.
//  2. Recipe when intent matches a viable recipe above MinRecipeConfidence.
//  3. Classic otherwise.
//
// The `ctx` argument is accepted for future recipe-corpus implementations
// that may issue network I/O; the in-memory RecipeCorpus used today ignores
// it. Keeping it on the signature avoids a breaking change later.
func SelectBootstrapRoute(
	_ context.Context,
	intent string,
	existing []platform.ServiceStack,
	recipes RecipeCorpus,
) (BootstrapRoute, *RecipeMatch, error) {
	if hasAdoptableService(existing) {
		return BootstrapRouteAdopt, nil, nil
	}
	if trimmed := strings.TrimSpace(intent); trimmed != "" && recipes != nil {
		match, err := recipes.FindViableMatch(trimmed)
		if err != nil {
			return "", nil, err
		}
		if match != nil && match.Confidence >= MinRecipeConfidence {
			return BootstrapRouteRecipe, match, nil
		}
	}
	return BootstrapRouteClassic, nil, nil
}

// hasAdoptableService reports whether `existing` contains at least one
// non-system, non-managed service. Such services drive BootstrapRouteAdopt: they
// exist without ServiceMeta and need the adopt flow to attach mode+strategy.
func hasAdoptableService(existing []platform.ServiceStack) bool {
	for _, svc := range existing {
		if svc.IsSystem() {
			continue
		}
		if IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			continue
		}
		return true
	}
	return false
}
