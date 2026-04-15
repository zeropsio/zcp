package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// RecipeFeature declares a single user-observable capability the recipe
// demonstrates. Features are the contract between research (plan) and
// verification (deploy curl sweep + browser walk + close re-verify).
// Every declared feature must be observable end-to-end — no feature is
// "implemented" until its HealthCheck passes the curl sweep AND its
// browser walk returns the MustObserve state change.
//
// This type was added after v18's search feature shipped broken: the
// seed silently failed to populate the Meilisearch index, the API 500'd
// on queries, and the frontend crashed on an undefined `.length`
// access. None of the existing verification layers (zerops.yaml sub-step,
// subagent attestation, zerops_browser snapshot, code-review subagent)
// caught it because none of them knew which features the recipe
// claimed to demonstrate. Features are the list every verification
// layer iterates against — declaration and observation are tied into
// one contract.
type RecipeFeature struct {
	// ID is a stable slug identifying this feature. Used by:
	//   - Scaffold subagent to emit data-feature="{id}" on the UI surface
	//   - Deploy feature-sweep validator to key the health results
	//   - Browser walk to target selectors
	//   - Code-review subagent to check implementation presence
	// Must match [a-z][a-z0-9-]* and be unique within the plan.
	ID string `json:"id" jsonschema:"Stable slug — lowercase alphanumeric with hyphens. Unique within the plan. Used as the data-feature HTML attribute value for UI surfaces and as the health-check key for API surfaces."`

	// Description is a one-line claim of what the feature demonstrates.
	// Minimum length 10 characters. Becomes the dashboard section label
	// and the README feature list entry.
	Description string `json:"description" jsonschema:"One-line human-readable claim describing what this feature demonstrates. Minimum 10 characters. Becomes the dashboard section heading and the feature's README entry."`

	// Surface lists the architectural layers the feature touches. Values
	// drive which verification mechanism runs:
	//   - "api"     — has an HTTP endpoint; HealthCheck required
	//   - "ui"      — dashboard section; UITestID + Interaction + MustObserve required
	//   - "worker"  — touches a background worker
	//   - "db"      — writes to the primary database
	//   - "cache"   — touches the cache/valkey service
	//   - "storage" — writes/reads object storage
	//   - "search"  — touches the search engine
	//   - "queue"   — publishes to the messaging broker
	//   - "mail"    — sends via the mail catcher
	Surface []string `json:"surface" jsonschema:"Architectural layers this feature touches. One or more of: api, ui, worker, db, cache, storage, search, queue, mail. Drives verification — features with 'api' must declare HealthCheck; features with 'ui' must declare UITestID, Interaction, and MustObserve."`

	// HealthCheck is the HTTP path the deploy curl sweep hits to prove
	// this feature's API surface responds with non-error. Required when
	// Surface contains "api". Must start with "/". The sweep enforces
	// 2xx status AND Content-Type starting with "application/json" —
	// the nginx HTML fallback trap that bit v18's search feature is
	// caught here.
	HealthCheck string `json:"healthCheck,omitempty" jsonschema:"REQUIRED when surface includes 'api'. HTTP path (starting with '/') the deploy curl sweep hits to prove this feature's API works. Example: '/api/items'. Enforcement: must return 2xx with Content-Type application/json — HTML fallbacks (SPA try_files) fail the sweep."`

	// UITestID is the value of the data-feature HTML attribute the
	// scaffold emits on this feature's dashboard section. The browser
	// walk uses it to locate the section. Required when Surface
	// contains "ui". Usually identical to ID.
	UITestID string `json:"uiTestId,omitempty" jsonschema:"REQUIRED when surface includes 'ui'. Value of the data-feature HTML attribute the scaffold emits so the browser walk can locate this feature's dashboard section. Usually identical to the feature ID."`

	// Interaction is the agent-authored description of how to exercise
	// this feature during the browser walk. Free-form but must name
	// the element to act on, the action (fill / click / type), and the
	// expected user flow. Required when Surface contains "ui".
	Interaction string `json:"interaction,omitempty" jsonschema:"REQUIRED when surface includes 'ui'. Agent-authored description of how the browser walk should exercise this feature. Names the element to act on, the action (fill/click/type), and the expected user flow."`

	// MustObserve names the post-interaction state that proves the
	// feature worked. Selector, text pattern, or countable assertion
	// the browser walk can verify. Required when Surface contains
	// "ui". The empty-state ambiguity is eliminated — "zero hits" is
	// a failure unless the MustObserve string explicitly declares
	// zero as valid (e.g. "zero hits is valid for empty query").
	MustObserve string `json:"mustObserve,omitempty" jsonschema:"REQUIRED when surface includes 'ui'. Post-interaction state that proves the feature worked. Selector, text pattern, or countable assertion the browser walk verifies. Empty/zero-state outcomes must be explicitly justified — 'no results' is a failure by default."`
}

// featureIDRegexp validates RecipeFeature.ID format — lowercase slug.
var featureIDRegexp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Feature surface constants. These are the layers a feature can touch.
// Kept small and framework-agnostic: "db" and "worker" are roles, not
// service types. Plan-target types (postgresql, nats, ...) live in
// Targets — Features describe the user-observable capabilities that
// exercise those targets.
const (
	FeatureSurfaceAPI     = "api"
	FeatureSurfaceUI      = "ui"
	FeatureSurfaceWorker  = "worker"
	FeatureSurfaceDB      = "db"
	FeatureSurfaceCache   = "cache"
	FeatureSurfaceStorage = "storage"
	FeatureSurfaceSearch  = "search"
	FeatureSurfaceQueue   = "queue"
	FeatureSurfaceMail    = "mail"
)

// allowedFeatureSurfaces is the full set of legal Surface values.
// Updating this set requires updating the validator message AND the
// scaffold brief's surface guidance AND the browser-walk rubric.
var allowedFeatureSurfaces = map[string]bool{
	FeatureSurfaceAPI:     true,
	FeatureSurfaceUI:      true,
	FeatureSurfaceWorker:  true,
	FeatureSurfaceDB:      true,
	FeatureSurfaceCache:   true,
	FeatureSurfaceStorage: true,
	FeatureSurfaceSearch:  true,
	FeatureSurfaceQueue:   true,
	FeatureSurfaceMail:    true,
}

// featureDescriptionMinLen is the floor on RecipeFeature.Description.
// 10 characters is enough to force "what does this demonstrate" prose
// beyond single-word placeholders ("items", "search") without forcing
// a paragraph. Trimmed before measurement.
const featureDescriptionMinLen = 10

// hasSurface reports whether the feature declares the given surface.
func (f RecipeFeature) hasSurface(surface string) bool {
	return slices.Contains(f.Surface, surface)
}

// validateFeatures enforces the feature contract at plan-submission time.
// Rules (in order):
//  1. At least one feature is declared.
//  2. Each feature has a unique lowercase-slug ID.
//  3. Description is >= featureDescriptionMinLen chars (trimmed).
//  4. Surface values come from allowedFeatureSurfaces.
//  5. surface "api"  => HealthCheck non-empty, starts with "/".
//  6. surface "ui"   => UITestID (slug), Interaction, MustObserve all non-empty.
//  7. Showcase tier: every required surface the plan's targets imply
//     must be covered by at least one feature. "api" and "ui" are always
//     required; managed service kinds map to surface names via
//     showcaseKindToSurface; a worker target forces "worker".
//
// Non-showcase tiers are only checked up to rule 6 — hello-world and
// minimal recipes declare as many features as they need without a
// managed-service coverage mandate.
func validateFeatures(features []RecipeFeature, tier string, targets []RecipeTarget) []string {
	if len(features) == 0 {
		return []string{"features is required (at least one) — every recipe must declare the capabilities it demonstrates"}
	}

	var errs []string
	seenID := make(map[string]int, len(features))
	for i, f := range features {
		errs = append(errs, validateOneFeature(i, f, seenID)...)
	}

	if tier == RecipeTierShowcase {
		errs = append(errs, validateShowcaseFeatureCoverage(features, targets)...)
	}

	return errs
}

// validateOneFeature checks a single feature's own fields. The seenID
// map tracks duplicates across all features in the plan — it's passed
// in rather than re-built so callers control its lifecycle.
func validateOneFeature(i int, f RecipeFeature, seenID map[string]int) []string {
	var errs []string

	// ID.
	switch {
	case f.ID == "":
		errs = append(errs, fmt.Sprintf("features[%d]: id is required", i))
	case !featureIDRegexp.MatchString(f.ID):
		errs = append(errs, fmt.Sprintf("features[%d] %q: id must match [a-z][a-z0-9-]* (lowercase slug)", i, f.ID))
	default:
		if prev, dup := seenID[f.ID]; dup {
			errs = append(errs, fmt.Sprintf("features[%d] %q: duplicate id (first at features[%d])", i, f.ID, prev))
		} else {
			seenID[f.ID] = i
		}
	}

	// Description.
	if len(strings.TrimSpace(f.Description)) < featureDescriptionMinLen {
		errs = append(errs, fmt.Sprintf("features[%d] %q: description too short (min %d chars) — state what the feature demonstrates", i, f.ID, featureDescriptionMinLen))
	}

	// Surface.
	if len(f.Surface) == 0 {
		errs = append(errs, fmt.Sprintf("features[%d] %q: surface is required (at least one of: api, ui, worker, db, cache, storage, search, queue, mail)", i, f.ID))
		return errs
	}
	for _, s := range f.Surface {
		if !allowedFeatureSurfaces[s] {
			errs = append(errs, fmt.Sprintf("features[%d] %q: surface %q not in allowed set (api, ui, worker, db, cache, storage, search, queue, mail)", i, f.ID, s))
		}
	}

	// api surface => HealthCheck.
	if f.hasSurface(FeatureSurfaceAPI) {
		switch {
		case f.HealthCheck == "":
			errs = append(errs, fmt.Sprintf("features[%d] %q: healthCheck is required when surface includes 'api' — the deploy curl sweep needs a path to verify this feature responds with JSON", i, f.ID))
		case !strings.HasPrefix(f.HealthCheck, "/"):
			errs = append(errs, fmt.Sprintf("features[%d] %q: healthCheck %q must start with '/' — it is an HTTP path on the API service", i, f.ID, f.HealthCheck))
		}
	}

	// ui surface => UITestID + Interaction + MustObserve.
	if f.hasSurface(FeatureSurfaceUI) {
		switch {
		case f.UITestID == "":
			errs = append(errs, fmt.Sprintf("features[%d] %q: uiTestId is required when surface includes 'ui' — scaffold emits data-feature=\"{id}\" for browser walk targeting", i, f.ID))
		case !featureIDRegexp.MatchString(f.UITestID):
			errs = append(errs, fmt.Sprintf("features[%d] %q: uiTestId %q must match [a-z][a-z0-9-]* (lowercase slug)", i, f.ID, f.UITestID))
		}
		if strings.TrimSpace(f.Interaction) == "" {
			errs = append(errs, fmt.Sprintf("features[%d] %q: interaction is required when surface includes 'ui' — name the element, action, and user flow the browser walk must perform", i, f.ID))
		}
		if strings.TrimSpace(f.MustObserve) == "" {
			errs = append(errs, fmt.Sprintf("features[%d] %q: mustObserve is required when surface includes 'ui' — name the state change that proves the feature worked. 'No results' is a failure by default.", i, f.ID))
		}
	}

	return errs
}

// showcaseKindToSurface maps managed-service kind constants (used by
// validateShowcaseServices) to the feature surface string that
// demonstrates that kind. Keeping this as a table and not inlining it
// means the same lookup drives both "what services must exist" and
// "what features must cover them" — one source of truth per kind.
var showcaseKindToSurface = map[string]string{
	kindDatabase:     FeatureSurfaceDB,
	kindCache:        FeatureSurfaceCache,
	kindStorage:      FeatureSurfaceStorage,
	kindSearchEngine: FeatureSurfaceSearch,
	kindMessaging:    FeatureSurfaceQueue,
	kindMailCatcher:  FeatureSurfaceMail,
}

// validateShowcaseFeatureCoverage enforces that every managed-service
// kind declared by the plan's targets is covered by at least one
// feature's surface list. Plus the always-required api + ui surfaces
// (showcase dashboards always expose both) and the worker surface
// when any runtime target is a worker.
//
// This is the rule that makes v18's search-broken-silently bug
// uncatchable in future runs: the plan has a meilisearch target →
// requiredSurfaces includes "search" → feature list must have a
// feature declaring "search" in its surface → that feature's
// HealthCheck hits the search endpoint in the curl sweep → the sweep
// rejects text/html responses → the silent index-not-populated
// failure becomes a deploy-step failure.
func validateShowcaseFeatureCoverage(features []RecipeFeature, targets []RecipeTarget) []string {
	requiredSurfaces := map[string]bool{
		FeatureSurfaceAPI: true,
		FeatureSurfaceUI:  true,
	}
	for _, t := range targets {
		if surface, ok := showcaseKindToSurface[serviceTypeKind(t.Type)]; ok {
			requiredSurfaces[surface] = true
		}
		if t.IsWorker && IsRuntimeType(t.Type) {
			requiredSurfaces[FeatureSurfaceWorker] = true
		}
	}

	covered := make(map[string]bool, len(requiredSurfaces))
	for _, f := range features {
		for _, s := range f.Surface {
			if requiredSurfaces[s] {
				covered[s] = true
			}
		}
	}

	// Deterministic order for error output — range over map is not.
	orderedSurfaces := []string{
		FeatureSurfaceAPI,
		FeatureSurfaceUI,
		FeatureSurfaceWorker,
		FeatureSurfaceDB,
		FeatureSurfaceCache,
		FeatureSurfaceStorage,
		FeatureSurfaceSearch,
		FeatureSurfaceQueue,
		FeatureSurfaceMail,
	}
	var errs []string
	for _, s := range orderedSurfaces {
		if requiredSurfaces[s] && !covered[s] {
			errs = append(errs, fmt.Sprintf("showcase features must cover the %q surface — plan has the corresponding service/role but no feature declares it", s))
		}
	}
	return errs
}
