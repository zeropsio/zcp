package workflow

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// dependencyAliases maps user-mentioned tokens to a normalized service-type
// family. The agent's intent text usually says "MySQL" / "Postgres" / "Valkey"
// in plain prose; recipes carry concrete type strings like "postgresql@18" or
// "valkey@7.2". Normalization lets the comparator treat both shapes uniformly
// without inventing a fake service catalog.
//
// Keep this list conservative — only well-known DB / cache / queue / search
// engine names. False positives here cause recipes to get demoted/dropped
// when they shouldn't; we'd rather miss a mismatch than drop a valid recipe.
//
//nolint:gochecknoglobals // value-only enum table, immutable.
var dependencyAliases = map[string]string{
	"postgres":   "postgresql",
	"postgresql": "postgresql",
	"pgsql":      "postgresql",

	"mysql":   "mariadb",
	"mariadb": "mariadb",

	"valkey": "valkey",
	"redis":  "valkey", // valkey is a redis-compatible fork; recipes use either token

	"keydb": "keydb",

	"mongodb": "mongodb",
	"mongo":   "mongodb",

	"meilisearch": "meilisearch",
	"meili":       "meilisearch",

	"rabbitmq": "rabbitmq",
	"nats":     "nats",

	"objectstorage": "object-storage",
	"s3":            "object-storage",
	"object":        "object-storage", // matches "object storage" with split tokens
}

// ExtractIntentDependencies tokenizes user intent text and returns the unique
// set of normalized dependency-family tokens recognised. Empty slice when no
// tokens match — the caller treats this as "no constraint expressed".
func ExtractIntentDependencies(intent string) []string {
	if intent == "" {
		return nil
	}
	lower := strings.ToLower(intent)
	seen := make(map[string]bool)
	var out []string
	// Tokenize on non-alphanumeric: split punctuation, hyphens, slashes.
	for _, raw := range tokenize(lower) {
		fam, ok := dependencyAliases[raw]
		if !ok {
			continue
		}
		if seen[fam] {
			continue
		}
		seen[fam] = true
		out = append(out, fam)
	}
	return out
}

// tokenize splits text on non-alphanumeric runes (keeps tokens like "appdev",
// "go1", "valkey7" intact while breaking at "/", "+", "-", "@", spaces).
func tokenize(s string) []string {
	out := make([]string, 0, 8)
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if alnum {
			if start == -1 {
				start = i
			}
			continue
		}
		if start != -1 {
			out = append(out, s[start:i])
			start = -1
		}
	}
	if start != -1 {
		out = append(out, s[start:])
	}
	return out
}

// RecipeServiceTypes parses recipe importYaml and returns the normalized
// service-type families declared in services[].type. Version suffixes are
// stripped (`postgresql@18` → `postgresql`). Unknown types fall through with
// the base token so the comparator can still report them in mismatches.
func RecipeServiceTypes(yamlBytes []byte) []string {
	if len(yamlBytes) == 0 {
		return nil
	}
	var doc struct {
		Services []struct {
			Type string `yaml:"type"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(yamlBytes, &doc); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, svc := range doc.Services {
		if svc.Type == "" {
			continue
		}
		base, _, _ := strings.Cut(svc.Type, "@")
		base = strings.ToLower(base)
		if fam, ok := dependencyAliases[base]; ok {
			base = fam
		}
		if seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, base)
	}
	return out
}

// ContradictionPair names a user-requested family and the family the recipe
// supplies in the same conceptual slot (e.g. user said "mysql", recipe has
// "postgresql"). Slots are inferred from a small allow-list of conflict
// groups — we only flag mismatches in the same conceptual category, not
// across categories.
type ContradictionPair struct {
	Wanted string // normalized family the user requested
	Got    string // normalized family the recipe supplies
}

// StackMismatch summarizes how a recipe's stack diverges from the user's
// intent. Workflow uses the three lists for different decisions:
//   - Contradicted: drop the recipe (wrong DB type at the same slot).
//   - MissingFromRecipe: demote (recipe lacks a thing the user asked for).
//   - Extras: informational only — recipe over-provisions.
type StackMismatch struct {
	Contradicted      []ContradictionPair
	MissingFromRecipe []string
	Extras            []string
}

// HasContradiction reports whether the recipe should be dropped from
// routeOptions.
func (m StackMismatch) HasContradiction() bool { return len(m.Contradicted) > 0 }

// HasMissing reports whether the recipe should be demoted (kept but ranked
// below classic).
func (m StackMismatch) HasMissing() bool { return len(m.MissingFromRecipe) > 0 }

// conflictGroups defines slots where alternative implementations are
// mutually exclusive. If user says "mysql" and recipe provisions "postgresql"
// in the same DB slot, that's a hard contradiction. Cross-group differences
// (e.g. user mentions Postgres + Redis; recipe has Postgres + Valkey + S3)
// don't contradict — the extras land in Extras for an informational note.
//
//nolint:gochecknoglobals // value-only enum table.
var conflictGroups = [][]string{
	{"postgresql", "mariadb", "mongodb"}, // primary database
	{"valkey", "keydb"},                  // cache/store
	{"meilisearch"},                      // search (single-engine for now)
	{"object-storage"},                   // blob storage (single-engine for now)
}

// CompareStacks computes the mismatch between user-mentioned dependencies
// and what a recipe's importYaml provisions. Both inputs are pre-normalized
// to dependency families.
func CompareStacks(intentDeps, recipeTypes []string) StackMismatch {
	intentSet := setOf(intentDeps)
	recipeSet := setOf(recipeTypes)

	var out StackMismatch
	// Contradicted: user wanted X in slot S; recipe has Y (≠X) in slot S.
	for _, group := range conflictGroups {
		var wanted, got string
		for _, fam := range group {
			if intentSet[fam] {
				wanted = fam
			}
			if recipeSet[fam] {
				got = fam
			}
		}
		if wanted != "" && got != "" && wanted != got {
			out.Contradicted = append(out.Contradicted, ContradictionPair{Wanted: wanted, Got: got})
		}
	}
	// MissingFromRecipe: user wanted X; recipe has nothing in X's slot at all.
	for _, fam := range intentDeps {
		if recipeSet[fam] {
			continue
		}
		if anyInSameGroup(fam, recipeSet) {
			continue // contradicted already covers this
		}
		out.MissingFromRecipe = append(out.MissingFromRecipe, fam)
	}
	// Extras: recipe has Y; user never mentioned anything in Y's slot.
	// Only count tokens that belong to a known dependency conflict group
	// (DB / cache / search / object-storage). Runtime types (python, nodejs,
	// php-nginx, …) are intentionally excluded — they're the runtime the
	// recipe uses, not over-provisioned dependencies.
	for _, fam := range recipeTypes {
		if !isKnownDependencyFamily(fam) {
			continue
		}
		if intentSet[fam] {
			continue
		}
		if anyInSameGroup(fam, intentSet) {
			continue // contradicted already covers this
		}
		out.Extras = append(out.Extras, fam)
	}
	return out
}

// isKnownDependencyFamily reports whether fam is one of the conflict-group
// dependency families (DB / cache / search / object-storage). Used to filter
// out runtime tokens (python, nodejs, …) from the Extras list — recipe's
// runtime is not an over-provisioned dependency.
func isKnownDependencyFamily(fam string) bool {
	for _, group := range conflictGroups {
		if slices.Contains(group, fam) {
			return true
		}
	}
	return false
}

func setOf(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func anyInSameGroup(fam string, set map[string]bool) bool {
	for _, group := range conflictGroups {
		if !slices.Contains(group, fam) {
			continue
		}
		for _, g := range group {
			if g == fam {
				continue
			}
			if set[g] {
				return true
			}
		}
	}
	return false
}
