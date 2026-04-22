package workflow

import (
	"context"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"gopkg.in/yaml.v3"
)

// BootstrapRoute is the bootstrap path chosen for a given intent + project state.
// The route is recorded once at bootstrap start and persists for the session.
type BootstrapRoute string

const (
	// BootstrapRouteRecipe — intent matches a viable recipe, infrastructure is empty (or compatible).
	BootstrapRouteRecipe BootstrapRoute = "recipe"
	// BootstrapRouteClassic — manual plan, always available as explicit override.
	BootstrapRouteClassic BootstrapRoute = "classic"
	// BootstrapRouteAdopt — project has runtime services without complete ServiceMeta.
	BootstrapRouteAdopt BootstrapRoute = "adopt"
	// BootstrapRouteResume — an earlier bootstrap left an incomplete ServiceMeta tagged
	// with a session ID; resuming that session is the only clean path.
	BootstrapRouteResume BootstrapRoute = "resume"
)

// RecipeMatch describes a recipe that scored above the noise floor against
// the user's intent. Matches are ranked and surfaced as route options; the
// LLM picks one (or none) explicitly.
//
// ImportYAML is the canonical project-import YAML the `zerops_import` tool
// should receive at the provision step. Carried through route selection so
// provision can inject it without a second store lookup.
type RecipeMatch struct {
	Slug        string  `json:"slug"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Confidence  float64 `json:"confidence"`
	ImportYAML  string  `json:"importYaml,omitempty"`
	// Mode is the bootstrap mode inferred from ImportYAML (standard, simple,
	// dev). Empty when the YAML shape is unrecognised or managed-only.
	Mode Mode `json:"mode,omitempty"`
}

// RecipeCorpus abstracts the recipe search surface. Implementations live in
// internal/knowledge; the interface here keeps route building testable
// without pulling the full knowledge engine into unit tests.
type RecipeCorpus interface {
	// FindRankedMatches returns up to `limit` recipe matches sorted by
	// confidence descending. Empty intent or an empty corpus MUST return an
	// empty slice (no error). Errors are reserved for genuine lookup
	// failures (network, parse, etc.) so callers can distinguish "no
	// matches" from "corpus broken".
	FindRankedMatches(intent string, limit int) ([]RecipeMatch, error)
}

// MinRecipeConfidence is the noise-floor confidence at which a candidate is
// even considered for inclusion in the options list. The engine no longer
// uses this as a hard gate — ranked candidates above this score are all
// surfaced so the LLM can decide. Tokens below 0.5 are almost always
// incidental substring matches without semantic relevance.
const MinRecipeConfidence = 0.5

// MaxRecipeOptions caps how many recipe candidates are surfaced in the
// discovery response. Three is enough to give the LLM alternatives without
// wasting tokens on weak candidates; anything past the top three almost
// always ties or falls below the noise floor.
const MaxRecipeOptions = 3

// BootstrapRouteOption is one candidate route surfaced in the discovery
// response. The engine ranks options and the LLM picks one by name (with
// the recipe slug as a secondary parameter when the route is recipe).
//
// The fields are scope-specific rather than polymorphic so JSON consumers
// (the LLM) never have to guess which slice a given list belongs to:
//
//   - AdoptServices is set only when Route == adopt.
//   - ResumeSession is set only when Route == resume.
//   - RecipeSlug/Confidence/ImportYAML/Collisions are set only when
//     Route == recipe.
//   - Why is always set — a one-sentence rationale the atom layer renders
//     for the LLM.
type BootstrapRouteOption struct {
	Route          BootstrapRoute `json:"route"`
	Why            string         `json:"why"`
	RecipeSlug     string         `json:"recipeSlug,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	ImportYAML     string         `json:"importYaml,omitempty"`
	Collisions     []string       `json:"collisions,omitempty"`
	AdoptServices  []string       `json:"adoptServices,omitempty"`
	ResumeSession  string         `json:"resumeSession,omitempty"`
	ResumeServices []string       `json:"resumeServices,omitempty"`
}

// BuildBootstrapRouteOptions is the discovery-phase entry point: it inspects
// the project state and the intent and returns a ranked list of actionable
// routes for the LLM to choose from. `classic` is always the final entry so
// the LLM can explicitly force a manual plan even when another route would
// auto-score higher.
//
// Ordering:
//
//  1. resume — when an earlier session left incomplete metas tagged with a
//     session ID. ZCP already owns those service slots; resuming is the
//     only clean path without manual cleanup.
//  2. adopt — when the project has runtime services without complete
//     ServiceMeta. Offered before recipe so the LLM doesn't silently
//     bootstrap-over unadopted services.
//  3. recipe — up to MaxRecipeOptions candidates ≥ MinRecipeConfidence,
//     sorted by confidence descending. Each is annotated with hostname
//     collisions against `existing` so the LLM can see at-a-glance which
//     recipes can actually import cleanly.
//  4. classic — always last, always present. The only route that never
//     auto-selects; the LLM picks it to force a manual plan.
//
// An intent string with no token matches produces a single-entry output
// `[classic]`. An empty project with empty intent produces the same. The
// function never returns an empty slice — classic is always included.
func BuildBootstrapRouteOptions(
	_ context.Context,
	intent string,
	existing []platform.ServiceStack,
	metas []*ServiceMeta,
	recipes RecipeCorpus,
) ([]BootstrapRouteOption, error) {
	var options []BootstrapRouteOption

	// Resume precedes adopt: ZCP already owns those slots via BootstrapSession.
	if opt, ok := resumeOption(existing, metas); ok {
		options = append(options, opt)
	}

	if opt, ok := adoptOption(existing, metas); ok {
		options = append(options, opt)
	}

	if strings.TrimSpace(intent) != "" && recipes != nil {
		matches, err := recipes.FindRankedMatches(intent, MaxRecipeOptions)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if m.Confidence < MinRecipeConfidence {
				continue
			}
			options = append(options, BootstrapRouteOption{
				Route:      BootstrapRouteRecipe,
				RecipeSlug: m.Slug,
				Confidence: m.Confidence,
				ImportYAML: m.ImportYAML,
				Why:        recipeWhy(m),
				Collisions: recipeCollisions(m.ImportYAML, existing),
			})
		}
	}

	options = append(options, BootstrapRouteOption{
		Route: BootstrapRouteClassic,
		Why:   "Manual plan — user describes services directly, no recipe template.",
	})

	return options, nil
}

// adoptOption returns an adopt route option when the project has at least
// one runtime service without a complete ServiceMeta AND with no incomplete
// ServiceMeta tagged to any session (those are resumable, not adoptable).
func adoptOption(existing []platform.ServiceStack, metas []*ServiceMeta) (BootstrapRouteOption, bool) {
	adoptable := adoptableServices(existing, metas)
	if len(adoptable) == 0 {
		return BootstrapRouteOption{}, false
	}
	return BootstrapRouteOption{
		Route:         BootstrapRouteAdopt,
		AdoptServices: adoptable,
		Why:           adoptWhy(adoptable),
	}, true
}

// resumeOption returns a resume route option when at least one ServiceMeta
// is incomplete AND carries a non-empty BootstrapSession. Resumable metas
// with empty BootstrapSession would be orphans — those fall under adopt.
//
// The ResumeSession field carries the session ID of the first resumable
// service; handlers look up the session state by that ID. Multiple incomplete
// metas with different sessions collapse into the first session — that's
// OK because a single bootstrap session always owns one plan, so all its
// partial metas share the same ID.
func resumeOption(existing []platform.ServiceStack, metas []*ServiceMeta) (BootstrapRouteOption, bool) {
	services, sessionID := resumableServices(existing, metas)
	if len(services) == 0 {
		return BootstrapRouteOption{}, false
	}
	return BootstrapRouteOption{
		Route:          BootstrapRouteResume,
		ResumeSession:  sessionID,
		ResumeServices: services,
		Why:            resumeWhy(services),
	}, true
}

// adoptableServices returns the hostnames of runtime services that lack a
// complete ServiceMeta and whose meta (if any) carries no BootstrapSession.
// Managed and system services are excluded — they are never adopted.
func adoptableServices(existing []platform.ServiceStack, metas []*ServiceMeta) []string {
	metaByHost := metaIndex(metas)
	var out []string
	for _, svc := range existing {
		if svc.IsSystem() {
			continue
		}
		if IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			continue
		}
		meta := metaByHost[svc.Name]
		if meta != nil && meta.IsComplete() {
			continue
		}
		// Incomplete meta with BootstrapSession tag is resumable, not adoptable.
		if meta != nil && meta.BootstrapSession != "" {
			continue
		}
		out = append(out, svc.Name)
	}
	return out
}

// resumableServices returns the hostnames of services whose ServiceMeta is
// incomplete AND tagged with a non-empty BootstrapSession. The first such
// session ID is returned alongside — callers use it to load the bootstrap
// session state.
func resumableServices(existing []platform.ServiceStack, metas []*ServiceMeta) ([]string, string) {
	metaByHost := metaIndex(metas)
	var out []string
	var sessionID string
	for _, svc := range existing {
		if svc.IsSystem() {
			continue
		}
		meta := metaByHost[svc.Name]
		if meta == nil || meta.IsComplete() {
			continue
		}
		if meta.BootstrapSession == "" {
			continue
		}
		out = append(out, svc.Name)
		if sessionID == "" {
			sessionID = meta.BootstrapSession
		}
	}
	return out, sessionID
}

// metaIndex builds a hostname → meta lookup covering both the primary
// hostname and any stage pair. Nil metas are skipped.
func metaIndex(metas []*ServiceMeta) map[string]*ServiceMeta {
	out := make(map[string]*ServiceMeta, len(metas))
	for _, m := range metas {
		if m == nil {
			continue
		}
		out[m.Hostname] = m
		if m.StageHostname != "" {
			out[m.StageHostname] = m
		}
	}
	return out
}

// recipeCollisions returns the hostnames that a recipe's ImportYAML would
// attempt to create but that already exist in the project. The check is a
// lightweight YAML parse — we only look at `services[].hostname`, no schema
// validation. Parse failures return nil (we don't block route selection
// on malformed ImportYAML; the provision step will catch it).
func recipeCollisions(importYAML string, existing []platform.ServiceStack) []string {
	if importYAML == "" || len(existing) == 0 {
		return nil
	}
	var doc struct {
		Services []struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(importYAML), &doc); err != nil {
		return nil
	}

	existingSet := make(map[string]struct{}, len(existing))
	for _, svc := range existing {
		if svc.IsSystem() {
			continue
		}
		existingSet[svc.Name] = struct{}{}
	}
	var out []string
	for _, svc := range doc.Services {
		if _, ok := existingSet[svc.Hostname]; ok {
			out = append(out, svc.Hostname)
		}
	}
	return out
}

// adoptWhy builds the rationale string for an adopt option. Kept next to
// the option builder so message shape stays consistent if the wording
// changes. Rendered verbatim by the bootstrap-route-options atom.
func adoptWhy(services []string) string {
	switch len(services) {
	case 1:
		return "Adopt existing runtime service `" + services[0] + "` — has no ZCP metadata."
	default:
		return "Adopt " + plural(len(services), "runtime service") +
			" already in the project without ZCP metadata."
	}
}

func resumeWhy(services []string) string {
	switch len(services) {
	case 1:
		return "Resume interrupted bootstrap for `" + services[0] +
			"` — an incomplete ServiceMeta is tagged to a previous session."
	default:
		return "Resume interrupted bootstrap — " +
			plural(len(services), "runtime service") +
			" carry incomplete ServiceMeta from a previous session."
	}
}

func recipeWhy(m RecipeMatch) string {
	if m.Description != "" {
		return m.Description
	}
	if m.Title != "" {
		return m.Title
	}
	return "Recipe `" + m.Slug + "` matches the intent."
}

// plural is a tiny English pluralizer so the Why messages scale without
// a dependency. Not i18n-safe — the option messages are always English.
func plural(n int, noun string) string {
	switch n {
	case 0, 1:
		return "1 " + noun
	default:
		return itoa(n) + " " + noun + "s"
	}
}

// itoa avoids importing strconv for a single conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
